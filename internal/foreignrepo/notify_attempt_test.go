package foreignrepo

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
)

type notifyTransport func(*http.Request) (*http.Response, error)

func (fn notifyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func peerResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func didResponse(t *testing.T, did syntax.DID, id, endpoint string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"id": did,
		"service": []map[string]string{
			{"id": id, "serviceEndpoint": endpoint, "type": "AtprotoPersonalDataServer"},
		},
	})
	require.NoError(t, err)
	return string(b)
}

func makeLease(t *testing.T, f fixture, owner syntax.DID) Delivery {
	t.Helper()
	commit, err := f.authored.Apply(
		t.Context(),
		inviteURI(owner, f.first, key),
		spaces.WriteCreate,
		invite("private invitation text"),
		"",
	)
	require.NoError(t, err)
	lease := takeOne(t, f.authored, time.Now())
	checkDelivery(t, lease, commit)
	return lease
}

func TestNotifyAttemptWireFromCommittedLease(t *testing.T) {
	for _, test := range []struct {
		owner       syntax.DID
		resolvedURL string
		absoluteID  bool
	}{
		{recipient, "https://plc.directory/did:plc:exampleforeign", false},
		{"did:web:recipient.example.com", "https://recipient.example.com/.well-known/did.json", true},
	} {
		t.Run(test.owner.String(), func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			lease := makeLease(t, f, test.owner)
			notice := lease.Notification()
			// Changing a caller's copy cannot change what gets signed.
			other := lease.Notification()
			other.Author = f.second
			require.NotEqual(t, other.Author, lease.Notification().Author)
			author, err := f.host.LookupDID(ctx, f.first)
			require.NoError(t, err)
			pub, err := author.PublicKey()
			require.NoError(t, err)
			id := "#atproto_pds"
			if test.absoluteID {
				id = test.owner.String() + id
			}
			doc := didResponse(t, test.owner, id, "https://pds.example/tenant")
			var requests []string
			var nonces []string
			n := newInviteNotifier(f.host)
			n.client.Transport = notifyTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				limit, ok := r.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(limit), 10*time.Second)
				if r.Method == http.MethodGet {
					require.Empty(t, r.Header.Get("Authorization"))
					require.Equal(t, test.resolvedURL, r.URL.String())
					return peerResponse(http.StatusOK, doc), nil
				}
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(
					t,
					"https://pds.example/tenant/xrpc/com.atproto.space.notifyRepoUpdate",
					r.URL.String(),
				)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.NoError(t, r.Body.Close())
				// Check the JSON shape and base64 directly, not through the
				// same AT Protocol bytes encoder the sender uses.
				require.Equal(t, map[string]any{
					"space":   "at:space/" + test.owner.String() + "/org.atgrp.inbox/self",
					"repo":    f.first.String(),
					"repoRev": notice.Rev.String(),
					"data": map[string]any{
						"$bytes": base64.RawStdEncoding.EncodeToString(notice.Digest[:]),
					},
				}, body)
				raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				require.True(t, ok)
				parts := strings.Split(raw, ".")
				require.Len(t, parts, 3)
				signature, err := base64.RawURLEncoding.DecodeString(parts[2])
				require.NoError(t, err)
				require.NoError(
					t,
					pub.HashAndVerifyLenient([]byte(parts[0]+"."+parts[1]), signature),
				)
				claims := jwt.MapClaims{}
				parsed, _, err := jwt.NewParser().ParseUnverified(raw, claims)
				require.NoError(t, err)
				require.Equal(t, "ES256", parsed.Header["alg"])
				require.Equal(t, f.first.String(), claims["iss"])
				require.Equal(t, test.owner.String()+"#atproto_pds", claims["aud"])
				require.Equal(t, "com.atproto.space.notifyRepoUpdate", claims["lxm"])
				iat, err := claims.GetIssuedAt()
				require.NoError(t, err)
				exp, err := claims.GetExpirationTime()
				require.NoError(t, err)
				require.True(t, exp.After(time.Now()))
				require.Equal(t, 30*time.Second, exp.Sub(iat.Time))
				nonce, ok := claims["jti"].(string)
				require.True(t, ok)
				require.NotEmpty(t, nonce)
				nonces = append(nonces, nonce)
				return peerResponse(http.StatusOK, ""), nil
			})

			for range 2 {
				require.Equal(t, attemptResult{Delivered, reasonAccepted}, n.attempt(ctx, lease))
			}
			require.Len(t, requests, 4)
			require.NotEqual(t, nonces[0], nonces[1])

			// The attempt did not acknowledge in storage. A new revision can
			// land before the caller completes the older accepted attempt.
			uri := inviteURI(test.owner, f.first, key)
			before, err := f.authored.Get(ctx, uri)
			require.NoError(t, err)
			newer, err := f.authored.Apply(ctx, uri, spaces.WriteDelete, nil, before.CID.String())
			require.NoError(t, err)
			applied, err := f.authored.Complete(ctx, lease, time.Now(), Delivered)
			require.NoError(t, err)
			require.False(t, applied)
			checkDelivery(t, takeOne(t, f.authored, time.Now()), newer)
		})
	}
}

func TestNotifyAttemptOutcomeAndResponseBound(t *testing.T) {
	f := newFixture(t)
	item := makeLease(t, f, recipient)
	doc := didResponse(t, recipient, "#atproto_pds", "https://pds.example")
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   attemptResult
	}{
		{"throttled", 429, "", attemptResult{Retry, reasonThrottled}},
		{"transient server", 503, "", attemptResult{Retry, reasonServer}},
		{"timed out", 408, "", attemptResult{Retry, reasonInterrupted}},
		{"unsupported", 404, `{"error":"MethodNotFound"}`, attemptResult{PermanentFailure, reasonUnsupported}},
		{"not implemented", 501, "", attemptResult{PermanentFailure, reasonUnsupported}},
		{"absent inbox", 400, `{"error":"SpaceNotFound"}`, attemptResult{PermanentFailure, reasonRejected}},
		{"auth", 401, "", attemptResult{PermanentFailure, reasonRejected}},
		{"policy", 403, `{"message":"not stored"}`, attemptResult{PermanentFailure, reasonRejected}},
		{"future revision", 400, `{"error":"FutureRev"}`, attemptResult{PermanentFailure, reasonRejected}},
		{"unexpected success", 202, "", attemptResult{PermanentFailure, reasonBadResponse}},
		{"wrong success body", 200, "{}", attemptResult{PermanentFailure, reasonBadResponse}},
		{"oversized body", 200, strings.Repeat("x", maxNotifyResponse+1), attemptResult{PermanentFailure, reasonBadResponse}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newInviteNotifier(f.host)
			n.client.Transport = notifyTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					return peerResponse(http.StatusOK, doc), nil
				}
				require.NoError(t, r.Body.Close())
				return peerResponse(tc.status, tc.body), nil
			})
			require.Equal(t, tc.want, n.attempt(t.Context(), item))
		})
	}
}

func TestNotifyAttemptRejectsWrongOrUnusableAuthority(t *testing.T) {
	f := newFixture(t)
	item := makeLease(t, f, recipient)
	service := `{"id":"#atproto_pds","serviceEndpoint":"https://pds.example","type":"AtprotoPersonalDataServer"}`
	for _, tc := range []struct {
		name string
		doc  string
		want attemptReason
	}{
		{"wrong DID", didResponse(t, "did:plc:other", "#atproto_pds", "https://pds.example"), reasonRecipient},
		{"wrong fragment", didResponse(t, recipient, "#habitat", "https://pds.example"), reasonRecipient},
		{"foreign service ID", didResponse(t, recipient, "did:plc:other#atproto_pds", "https://pds.example"), reasonRecipient},
		{"duplicate service", `{"id":"did:plc:exampleforeign","service":[` + service + "," + service + "]}", reasonRecipient},
		{"wrong service type", strings.ReplaceAll(didResponse(t, recipient, "#atproto_pds", "https://pds.example"), "AtprotoPersonalDataServer", "Unknown"), reasonRecipient},
		{"large DID document", strings.Repeat("x", maxDIDDocument+1), reasonRecipient},
		{"cleartext", didResponse(t, recipient, "#atproto_pds", "http://pds.example"), reasonUnsafeEndpoint},
		{"loopback", didResponse(t, recipient, "#atproto_pds", "https://127.0.0.1"), reasonUnsafeEndpoint},
		{"IPv4-mapped loopback", didResponse(t, recipient, "#atproto_pds", "https://[::ffff:127.0.0.1]"), reasonUnsafeEndpoint},
		{"userinfo", didResponse(t, recipient, "#atproto_pds", "https://x@pds.example"), reasonUnsafeEndpoint},
		{"query", didResponse(t, recipient, "#atproto_pds", "https://pds.example?q=1"), reasonUnsafeEndpoint},
		{"fragment", didResponse(t, recipient, "#atproto_pds", "https://pds.example#x"), reasonUnsafeEndpoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newInviteNotifier(f.host)
			n.client.Transport = notifyTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Empty(t, r.Header.Get("Authorization"))
				return peerResponse(http.StatusOK, tc.doc), nil
			})
			require.Equal(t, attemptResult{PermanentFailure, tc.want}, n.attempt(t.Context(), item))
		})
	}
}

func TestNotifyAttemptNeverFollowsRedirect(t *testing.T) {
	f := newFixture(t)
	item := makeLease(t, f, recipient)
	doc := didResponse(t, recipient, "#atproto_pds", "https://pds.example")
	for _, from := range []string{http.MethodGet, http.MethodPost} {
		t.Run(from, func(t *testing.T) {
			var hosts []string
			n := newInviteNotifier(f.host)
			n.client.Transport = notifyTransport(func(r *http.Request) (*http.Response, error) {
				hosts = append(hosts, r.URL.Host)
				require.NotEqual(t, "unrelated.example", r.URL.Host)
				if r.Body != nil {
					require.NoError(t, r.Body.Close())
				}
				if r.Method == from {
					resp := peerResponse(http.StatusTemporaryRedirect, "")
					resp.Header.Set("Location", "https://unrelated.example/elsewhere")
					return resp, nil
				}
				return peerResponse(http.StatusOK, doc), nil
			})
			require.Equal(
				t,
				attemptResult{PermanentFailure, reasonUnsafeEndpoint},
				n.attempt(t.Context(), item),
			)
			want := []string{"plc.directory", "pds.example"}
			if from == http.MethodGet {
				want = want[:1]
			}
			require.True(t, slices.Equal(want, hosts))
		})
	}
}

func TestNotifyAttemptBoundsResolutionAndSubmissionByLease(t *testing.T) {
	f := newFixture(t)
	item := makeLease(t, f, recipient)
	doc := didResponse(t, recipient, "#atproto_pds", "https://pds.example")
	for _, blocked := range []string{http.MethodGet, http.MethodPost} {
		t.Run(blocked, func(t *testing.T) {
			n := newInviteNotifier(f.host)
			short := item
			short.leaseUntil = time.Now().Add(200 * time.Millisecond).UnixMilli()
			n.client.Transport = notifyTransport(func(r *http.Request) (*http.Response, error) {
				limit, ok := r.Context().Deadline()
				require.True(t, ok)
				require.Equal(t, short.leaseUntil, limit.UnixMilli())
				if r.Body != nil {
					require.NoError(t, r.Body.Close())
				}
				if r.Method != blocked {
					require.Equal(t, http.MethodGet, r.Method)
					return peerResponse(http.StatusOK, doc), nil
				}
				<-r.Context().Done()
				require.ErrorIs(t, r.Context().Err(), context.DeadlineExceeded)
				return nil, r.Context().Err()
			})
			why := reasonResolve
			if blocked == http.MethodPost {
				why = reasonInterrupted
			}
			require.Equal(t, attemptResult{Retry, why}, n.attempt(t.Context(), short))
		})
	}
}

func TestNotifyAttemptRejectsMissingCustodyOrExpiredLeaseBeforeIO(t *testing.T) {
	f := newFixture(t)
	item := makeLease(t, f, recipient)
	n := newInviteNotifier(f.host)
	n.client.Transport = notifyTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected network request")
		return nil, nil
	})
	remote := item
	remote.notice.Author = "did:plc:nonlocal"
	require.Equal(
		t,
		attemptResult{PermanentFailure, reasonLocalCustody},
		n.attempt(t.Context(), remote),
	)
	localOwner := item
	localOwner.notice.Space = inbox(f.second)
	require.Equal(
		t,
		attemptResult{PermanentFailure, reasonInvalidScope},
		n.attempt(t.Context(), localOwner),
	)
	expired := item
	expired.leaseUntil = time.Now().Add(-time.Second).UnixMilli()
	require.Equal(t, attemptResult{Retry, reasonLeaseExpired}, n.attempt(t.Context(), expired))
	require.Equal(
		t,
		attemptResult{PermanentFailure, reasonInvalidScope},
		n.attempt(t.Context(), Delivery{}),
	)
}

func TestNotifyAttemptPublicClientBlocksPrivateDial(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("public notification client reached a loopback server")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	n := newInviteNotifier(f.host)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := n.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("public client did not reject loopback")
	}
	require.ErrorContains(t, err, "non-public address")
	require.True(t, unsafeNotifyTransport(err))
	require.True(t, unsafeNotifyTransport(&tls.CertificateVerificationError{}))
}
