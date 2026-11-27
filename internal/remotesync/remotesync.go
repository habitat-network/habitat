// Package remotesync fetches the records of a space's external writers onto
// the space host. An external writer is a member whose records live on its
// own PDS rather than on this host (a Remote repo in package spaces): its PDS
// only tells the space host, through notifyWrite, that it advanced. To carry
// those records in the space's event log (network.habitat.space.subscribe),
// the space host pulls them itself.
//
// The space host is the space's authority, so it needs no one's delegation to
// read the space: it mints its own space credential, signed with the space
// owner's #atproto key and bound to a P-256 key the [Syncer] holds, and reads
// the writer's ops from its PDS with com.atproto.space.listRepoOps, like any
// other syncer. Each page asks for a MAC keyed to the writer's #atproto_kem
// key, and the ops are folded into the writer's LtHash, which must match the
// MAC-authenticated commit at the head of the oplog before any of them is
// stored. A writer whose ops don't add up is rebuilt from the start of its
// oplog.
package remotesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/httpsig"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/utils"
)

var tracer = otel.Tracer("github.com/habitat-network/habitat/internal/remotesync")

// Store is the part of [spaces.Store] the [Syncer] keeps external writers'
// records in.
type Store interface {
	GetRemoteRepo(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (spaces.RemoteRepo, error)
	ListRemoteReposBehind(ctx context.Context) ([]spaces.RemoteRepo, error)
	ApplyRemoteSync(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		sync spaces.RemoteSync,
	) error
}

// OwnerKeys resolves the private key of a space owner hosted here, which
// signs the owner's space credentials. hive.Hive satisfies it.
type OwnerKeys interface {
	PrivateKeyForDID(ctx context.Context, did syntax.DID) (atcrypto.PrivateKey, error)
}

// ErrInvalidOps is returned by [Syncer.Sync] when a writer's ops don't match
// the commit its PDS authenticated, even after a rebuild from the start of
// its oplog.
var ErrInvalidOps = errors.New("remote ops do not match the repo's commit")

const (
	// DefaultSweepInterval is how often [Syncer.Run] looks for writers that
	// are behind, such as those whose fetch failed or whose notifyWrite came
	// in while pear was down.
	DefaultSweepInterval = 5 * time.Minute
	// pageSize is how many ops one listRepoOps page asks for.
	pageSize = 500
	// maxOps bounds how many ops one sync fetches before it stores any, since
	// none is stored until the whole run is verified against the commit.
	maxOps      = 100_000
	listRepoOps = "com.atproto.space.listRepoOps"
)

// repoKey names one external writer of one space.
type repoKey struct {
	space habitat_syntax.SpaceURI
	repo  syntax.DID
}

// Syncer fetches external writers' records, one writer at a time, when
// [Syncer.Queue]d and in a periodic sweep.
type Syncer struct {
	store    Store
	dir      identity.Directory
	keys     OwnerKeys
	hostKey  atcrypto.PrivateKey
	httpc    *http.Client
	notifier spaces.Notifier
	// bindKey is the P-256 key the syncer's space credentials are bound to
	// (cnf.kid): every request presenting one is signed with it.
	bindKey       *atcrypto.PrivateKeyP256
	sweepInterval time.Duration

	mu      sync.Mutex
	order   []repoKey
	pending map[repoKey]bool
	wake    chan struct{}
}

// Option configures a [Syncer].
type Option func(*Syncer)

// WithSweepInterval sets how often [Syncer.Run] sweeps for writers that are
// behind.
func WithSweepInterval(d time.Duration) Option {
	return func(s *Syncer) { s.sweepInterval = d }
}

// New returns a Syncer storing into store. It resolves writers' PDSes and
// keys with dir, signs space credentials with the owner's key from keys, or
// with hostKey for spaces the host itself owns (as getSpaceCredential does),
// and tells notifier of every write it stores, so subscribe streams wake.
func New(
	store Store,
	dir identity.Directory,
	keys OwnerKeys,
	hostKey atcrypto.PrivateKey,
	httpc *http.Client,
	notifier spaces.Notifier,
	opts ...Option,
) (*Syncer, error) {
	bindKey, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		return nil, fmt.Errorf("generate credential key: %w", err)
	}
	s := &Syncer{
		store:         store,
		dir:           dir,
		keys:          keys,
		hostKey:       hostKey,
		httpc:         httpc,
		notifier:      notifier,
		bindKey:       bindKey,
		sweepInterval: DefaultSweepInterval,
		pending:       map[repoKey]bool{},
		wake:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Queue asks for repo's records in space to be fetched. It never blocks: a
// writer already queued is queued once.
func (s *Syncer) Queue(space habitat_syntax.SpaceURI, repo syntax.DID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := repoKey{space: space, repo: repo}
	if !s.pending[key] {
		s.pending[key] = true
		s.order = append(s.order, key)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// next pops the oldest queued writer.
func (s *Syncer) next() (repoKey, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.order) == 0 {
		return repoKey{}, false
	}
	key := s.order[0]
	s.order = s.order[1:]
	delete(s.pending, key)
	return key, true
}

// Run fetches queued writers until ctx ends, sweeping for writers that are
// behind at start and every sweep interval. A failed fetch is logged and left
// for the next sweep.
func (s *Syncer) Run(ctx context.Context) error {
	sweep := time.NewTicker(s.sweepInterval)
	defer sweep.Stop()
	s.sweep(ctx)
	for {
		for {
			key, ok := s.next()
			if !ok {
				break
			}
			if err := s.Sync(ctx, key.space, key.repo); err != nil {
				if ctx.Err() != nil {
					return context.Cause(ctx)
				}
				slog.WarnContext(ctx, "remotesync: fetch external writer",
					"err", err, "space", key.space, "repo", key.repo)
			}
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-s.wake:
		case <-sweep.C:
			s.sweep(ctx)
		}
	}
}

// sweep queues every writer that is behind.
func (s *Syncer) sweep(ctx context.Context) {
	repos, err := s.store.ListRemoteReposBehind(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "remotesync: list writers behind", "err", err)
		return
	}
	for _, r := range repos {
		s.Queue(r.Space, r.Repo)
	}
}

// Sync fetches the ops repo has written to space since it was last fetched,
// verifies them against the commit its PDS authenticates, and stores them. A
// repo that is up to date, or isn't an external writer of space, is left
// alone.
func (s *Syncer) Sync(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (err error) {
	ctx, span := tracer.Start(ctx, "remotesync.Sync", trace.WithAttributes(
		telemetry.Space(space),
		telemetry.Repo(repo),
	))
	defer func() { telemetry.End(span, err) }()
	remote, err := s.store.GetRemoteRepo(ctx, space, repo)
	if errors.Is(err, spaces.ErrRepoNotRemote) {
		return nil
	} else if err != nil {
		return err
	}
	if !remote.Behind() {
		return nil
	}
	reader, err := s.reader(ctx, space, repo)
	if err != nil {
		return err
	}

	result, err := reader.fetch(ctx, remote.SyncedRev, remote.State)
	if remote.SyncedRev != "" && (errors.Is(err, errRevNotFound) || errors.Is(err, ErrInvalidOps)) {
		// The stored position doesn't lead to the repo's head: start over.
		span.AddEvent(
			"rebuild",
			trace.WithAttributes(attribute.String("habitat.cause", err.Error())),
		)
		result, err = reader.fetch(ctx, "", spacecommit.LtHash{})
	}
	if err != nil {
		return err
	}
	if result.Rev == "" {
		// An empty repo: nothing to store.
		return nil
	}
	span.SetAttributes(attribute.Int("habitat.ops", len(result.Ops)))
	if err := s.store.ApplyRemoteSync(ctx, space, repo, result); err != nil {
		return err
	}
	s.notifier.NotifyWrite(ctx, space, repo, result.Rev, result.State.Sum(), "", "")
	return nil
}

// reader prepares the requests for repo's ops in space: its PDS, its KEM
// key, and a fresh space credential.
func (s *Syncer) reader(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (*repoReader, error) {
	ident, err := s.dir.LookupDID(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", repo, err)
	}
	pds := ident.PDSEndpoint()
	if pds == "" {
		return nil, fmt.Errorf("%s has no PDS", repo)
	}
	// The writer's PDS keys MACs with the writer's own KEM key.
	kem, err := ident.GetPublicKey(spacecommit.KEMFragment)
	if err != nil {
		return nil, fmt.Errorf("%s kem key: %w", repo, err)
	}
	ownerKey, err := s.keys.PrivateKeyForDID(ctx, space.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		// Spaces owned by the pear host itself are signed with the host key.
		ownerKey = s.hostKey
	} else if err != nil {
		return nil, fmt.Errorf("space owner key: %w", err)
	}
	bindPub, err := s.bindKey.PublicKey()
	if err != nil {
		return nil, err
	}
	credential, err := utils.SpaceCredential(
		ownerKey, space, utils.WithConfirmationKey(bindPub.DIDKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("mint space credential: %w", err)
	}
	return &repoReader{
		httpc:      s.httpc,
		pds:        pds,
		space:      space,
		repo:       repo,
		kem:        kem,
		credential: credential,
		bindKey:    s.bindKey,
	}, nil
}

// errRevNotFound is a listRepoOps RevNotFound: since is past the repo's
// head, so the stored position is no good.
var errRevNotFound = errors.New("since revision is ahead of the repo head")

// repoReader reads one writer's ops from its PDS.
type repoReader struct {
	httpc      *http.Client
	pds        string
	space      habitat_syntax.SpaceURI
	repo       syntax.DID
	kem        atcrypto.PublicKey
	credential string
	bindKey    *atcrypto.PrivateKeyP256
}

// fetch reads every op after since, folding them into state, and returns
// them once the folded state matches the repo's authenticated commit. An
// empty since reads the whole oplog, as a rebuild.
func (r *repoReader) fetch(
	ctx context.Context,
	since syntax.TID,
	state spacecommit.LtHash,
) (spaces.RemoteSync, error) {
	result := spaces.RemoteSync{Rev: since, State: state, Rebuild: since == ""}
	var head *spacecommit.Commit
	for {
		page, commit, err := r.listRepoOps(ctx, result.Rev)
		if err != nil {
			return spaces.RemoteSync{}, err
		}
		for _, entry := range page.Ops {
			op, err := remoteOp(entry)
			if err != nil {
				return spaces.RemoteSync{}, err
			}
			if op.Prev != "" {
				result.State.Remove(spacecommit.RecordElement(op.Collection, op.Rkey, op.Prev))
			}
			if op.Cid != "" {
				result.State.Add(spacecommit.RecordElement(op.Collection, op.Rkey, op.Cid))
			}
			result.Ops = append(result.Ops, op)
			result.Rev = op.Rev
		}
		if len(result.Ops) > maxOps {
			return spaces.RemoteSync{}, fmt.Errorf("more than %d ops to fetch", maxOps)
		}
		if commit != nil {
			head = commit
		}
		if page.Cursor == "" || len(page.Ops) == 0 {
			break
		}
	}
	if head == nil {
		// The oplog's head was never reached: the repo has no commit yet.
		return result, nil
	}
	if !head.Matches(&result.State) {
		return spaces.RemoteSync{}, ErrInvalidOps
	}
	if rev, err := syntax.ParseTID(head.Rev); err == nil && rev > result.Rev {
		result.Rev = rev
	}
	return result, nil
}

// listRepoOps reads one page of ops after since, returning the commit when
// the page reaches the oplog's head, authenticated by the page's MAC.
func (r *repoReader) listRepoOps(
	ctx context.Context,
	since syntax.TID,
) (comatproto.ComAtprotoSpaceListRepoOpsOutput, *spacecommit.Commit, error) {
	var out comatproto.ComAtprotoSpaceListRepoOpsOutput
	keyShare, secret, err := spacecommit.Encapsulate(r.kem)
	if err != nil {
		return out, nil, fmt.Errorf("encapsulate: %w", err)
	}
	wantMAC := spacecommit.FormatMACRequest(spacecommit.CommitMACType, keyShare)
	q := url.Values{
		// Alpha PDSes require the at:space/ form.
		"space": {r.space.AtSpace().String()},
		"repo":  {r.repo.String()},
		"limit": {strconv.Itoa(pageSize)},
	}
	if since != "" {
		q.Set("since", since.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.pds+"/xrpc/"+listRepoOps+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return out, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", httpsig.CredentialScheme+" "+r.credential)
	// Repo methods are presented to the repo they name.
	req.Header.Set(httpsig.AudienceHeader, r.repo.String())
	req.Header.Set(spacecommit.AcceptSignatureHeader, wantMAC)
	if err := httpsig.Sign(req, r.bindKey, "authorization", "atproto-space-audience"); err != nil {
		return out, nil, fmt.Errorf("sign request: %w", err)
	}
	resp, err := r.httpc.Do(req)
	if err != nil {
		return out, nil, fmt.Errorf("list repo ops: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Error == "RevNotFound" {
			return out, nil, errRevNotFound
		}
		return out, nil, fmt.Errorf("list repo ops: %s: %s %s",
			resp.Status, body.Error, body.Message)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, nil, fmt.Errorf("decode list repo ops: %w", err)
	}
	if out.Commit == nil {
		return out, nil, nil
	}
	commit := spacecommit.FromComAtprotoXRPC(*out.Commit)
	if err := commit.Verify(r.space, r.repo, resp.Header, keyShare, secret); err != nil {
		return out, nil, err
	}
	return out, &commit, nil
}

// remoteOp parses one listRepoOps entry, checking that a record's value is
// the one its CID names, since only the CIDs are covered by the commit.
func remoteOp(entry comatproto.ComAtprotoSpaceListRepoOpsOpEntry) (spaces.RemoteOp, error) {
	rev, err := syntax.ParseTID(entry.Rev)
	if err != nil {
		return spaces.RemoteOp{}, fmt.Errorf("op rev %q: %w", entry.Rev, err)
	}
	collection, err := syntax.ParseNSID(entry.Collection)
	if err != nil {
		return spaces.RemoteOp{}, fmt.Errorf("op collection %q: %w", entry.Collection, err)
	}
	rkey, err := syntax.ParseRecordKey(entry.Rkey)
	if err != nil {
		return spaces.RemoteOp{}, fmt.Errorf("op rkey %q: %w", entry.Rkey, err)
	}
	op := spaces.RemoteOp{
		Rev:        rev,
		Collection: collection,
		Rkey:       rkey,
	}
	// cid is null for a delete, and prev for a create.
	if entry.Prev != nil {
		op.Prev = *entry.Prev
	}
	if entry.Cid == nil || *entry.Cid == "" {
		return op, nil
	}
	if entry.Value == nil {
		return spaces.RemoteOp{}, fmt.Errorf("op %s/%s has no value", collection, rkey)
	}
	raw, err := json.Marshal(entry.Value)
	if err != nil {
		return spaces.RemoteOp{}, err
	}
	data, err := atdata.UnmarshalJSON(raw)
	if err != nil {
		return spaces.RemoteOp{}, fmt.Errorf("op %s/%s value: %w", collection, rkey, err)
	}
	encoded, err := atdata.MarshalCBOR(data)
	if err != nil {
		return spaces.RemoteOp{}, fmt.Errorf("op %s/%s value: %w", collection, rkey, err)
	}
	sum, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(encoded)
	if err != nil {
		return spaces.RemoteOp{}, err
	}
	op.Cid = *entry.Cid
	if sum.String() != op.Cid {
		return spaces.RemoteOp{}, fmt.Errorf("%w: op %s/%s value doesn't match its cid",
			ErrInvalidOps, collection, rkey)
	}
	op.Value = encoded
	return op, nil
}
