package spacestream

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spacescope"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	alice  = syntax.DID("did:plc:alice")
	org    = syntax.DID("did:plc:org")
	spaceA = habitat_syntax.ConstructSpaceURI(org, "network.habitat.group", "a")
	spaceB = habitat_syntax.ConstructSpaceURI(org, "network.habitat.group", "b")
	spaceC = habitat_syntax.ConstructSpaceURI(alice, "network.habitat.group", "c")
)

// fakeLog is an in-memory [Source]. Events are appended already sequenced;
// it applies only the Spaces filter, which is all these tests use.
type fakeLog struct {
	mu     sync.Mutex
	events []spaces.Event
}

func (l *fakeLog) SequenceEvents(context.Context) (spaces.EventSeq, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return 0, nil
	}
	return l.events[len(l.events)-1].EventSeq(), nil
}

func (l *fakeLog) ListEvents(
	_ context.Context,
	filter spaces.EventFilter,
	cursor spaces.EventSeq,
	limit int,
) ([]spaces.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []spaces.Event
	for _, e := range l.events {
		if e.EventSeq() < cursor {
			continue
		}
		if len(filter.Spaces) > 0 && !slices.Contains(filter.Spaces, e.EventSpace()) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// add appends alice's commit in space at the next seq.
func (l *fakeLog) add(space habitat_syntax.SpaceURI, rkey syntax.RecordKey) {
	l.addBy(space, rkey, alice)
}

// addBy appends repo's commit in space at the next seq.
func (l *fakeLog) addBy(space habitat_syntax.SpaceURI, rkey syntax.RecordKey, repo syntax.DID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, spaces.CommitEvent{
		Seq:        spaces.EventSeq(len(l.events) + 1),
		Space:      space,
		Repo:       repo,
		Collection: "network.habitat.note",
		Rkey:       rkey,
		Rev:        "3l3qo2vutsw2b",
		Operation:  spaces.WriteCreate,
		Value:      map[string]any{"text": string(rkey)},
		Time:       time.Unix(0, 0),
	})
}

// deleteSpace appends space's deletion at the next seq.
func (l *fakeLog) deleteSpace(space habitat_syntax.SpaceURI) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, spaces.SpaceDeletedEvent{
		Seq:   spaces.EventSeq(len(l.events) + 1),
		Space: space,
		Time:  time.Unix(0, 0),
	})
}

// fakeRoles grants alice the reader role on the spaces in readable.
type fakeRoles struct {
	mu       sync.Mutex
	readable map[habitat_syntax.SpaceURI]bool
}

func (r *fakeRoles) CheckUserHasSpaceRole(
	_ context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	_ habitat_syntax.SpaceRole,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return did == alice && r.readable[space], nil
}

func (r *fakeRoles) set(space habitat_syntax.SpaceURI, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readable[space] = ok
}

// frame is a decoded xrpc.v1.json frame.
type frame struct {
	Type    string `json:"$type"`
	Error   string `json:"error"`
	Payload struct {
		Type   string `json:"$type"`
		Seq    int64  `json:"seq"`
		Space  string `json:"space"`
		Rkey   string `json:"rkey"`
		Name   string `json:"name"`
		Record struct {
			Text string `json:"text"`
		} `json:"record"`
	} `json:"payload"`
}

// fakeConn collects the frames written to it.
type fakeConn struct {
	mu     sync.Mutex
	frames []frame
}

func (c *fakeConn) WriteFrame(_ context.Context, raw []byte) error {
	var f frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, f)
	return nil
}

// take returns and clears the frames written so far.
func (c *fakeConn) take() []frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.frames
	c.frames = nil
	return f
}

// summary renders frames as "<kind> <seq> <rkey|space|name>" for comparison.
func summary(frames []frame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		switch f.Payload.Type {
		case "network.habitat.space.subscribe#commit":
			out[i] = "commit " + strconv.FormatInt(f.Payload.Seq, 10) + " " + f.Payload.Rkey
		case "network.habitat.space.subscribe#spaceDeleted":
			out[i] = "deleted " + strconv.FormatInt(f.Payload.Seq, 10) + " " + f.Payload.Space
		case "network.habitat.space.subscribe#info":
			out[i] = "info " + f.Payload.Name
		default:
			out[i] = f.Type + " " + f.Error
		}
	}
	return out
}

// setupStream runs sub on a stream over log in the current synctest bubble,
// returning the stream's frames and a function that ends it and returns
// Run's result.
func setupStream(
	t *testing.T,
	log *fakeLog,
	roles *fakeRoles,
	broadcaster *Broadcaster,
	sub Subscription,
	opts ...Option,
) (*fakeConn, func() error) {
	t.Helper()
	sub, err := Prepare(sub)
	require.NoError(t, err)
	server := NewServer(log, roles, broadcaster, opts...)
	conn := &fakeConn{}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- server.Run(ctx, conn, sub) }()
	synctest.Wait()
	return conn, func() error {
		cancel()
		return <-result
	}
}

func notify(b *Broadcaster) {
	b.NotifyWrite(context.Background(), spaceA, alice, "", nil, "", "")
	synctest.Wait()
}

func TestStreamReplaysThenFollowsAuthorizedSpaces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := &fakeLog{}
		roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}}
		broadcaster := NewBroadcaster()
		log.add(spaceA, "1")
		log.add(spaceB, "2")
		log.add(spaceA, "3")

		// A small page size, so the replay spans pages.
		conn, stop := setupStream(t, log, roles, broadcaster, Subscription{
			Query:  Query{Cursor: new(spaces.EventSeq(0))},
			Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
		}, WithPageSize(2))
		frames := conn.take()
		require.Equal(t, []string{"commit 1 1", "commit 3 3"}, summary(frames),
			"replay carries only the spaces alice can read")
		require.Equal(t, "1", frames[0].Payload.Record.Text)

		log.add(spaceA, "4")
		log.add(spaceB, "5")
		notify(broadcaster)
		require.Equal(t, []string{"commit 4 4"}, summary(conn.take()))

		// Losing access stops the flow, and regaining it resumes it.
		roles.set(spaceA, false)
		log.add(spaceA, "6")
		notify(broadcaster)
		require.Empty(t, conn.take())
		roles.set(spaceA, true)
		log.add(spaceA, "7")
		notify(broadcaster)
		require.Equal(t, []string{"commit 7 7"}, summary(conn.take()))

		require.ErrorIs(t, stop(), context.Canceled)
	})
}

func TestStreamCursor(t *testing.T) {
	tests := []struct {
		name   string
		cursor *spaces.EventSeq
		want   []string
	}{
		{name: "omitted starts at the live tip", want: []string{"commit 3 3"}},
		{
			name:   "inclusive",
			cursor: new(spaces.EventSeq(2)),
			want:   []string{"commit 2 2", "commit 3 3"},
		},
		{
			name:   "next seq",
			cursor: new(spaces.EventSeq(3)),
			want:   []string{"commit 3 3"},
		},
		{
			name:   "future starts at the live tip",
			cursor: new(spaces.EventSeq(10)),
			want:   []string{"info FutureCursor", "commit 3 3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := &fakeLog{}
				roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}}
				broadcaster := NewBroadcaster()
				log.add(spaceA, "1")
				log.add(spaceA, "2")
				conn, stop := setupStream(t, log, roles, broadcaster, Subscription{
					Query:  Query{Cursor: tt.cursor},
					Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
				})
				log.add(spaceA, "3")
				notify(broadcaster)
				require.Equal(t, tt.want, summary(conn.take()))
				require.ErrorIs(t, stop(), context.Canceled)
			})
		})
	}
}

func TestStreamPollsWithoutWake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := &fakeLog{}
		roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}}
		conn, stop := setupStream(t, log, roles, NewBroadcaster(), Subscription{
			Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
		}, WithPollInterval(time.Second))

		// A write another pear instance made: no wake reaches this one.
		log.add(spaceA, "1")
		synctest.Wait()
		require.Empty(t, conn.take())
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, []string{"commit 1 1"}, summary(conn.take()))
		require.ErrorIs(t, stop(), context.Canceled)
	})
}

func TestStreamSpaceCredentialEndsAtExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := &fakeLog{}
		broadcaster := NewBroadcaster()
		log.add(spaceA, "1")
		log.add(spaceB, "2")
		conn, stop := setupStream(t, log, &fakeRoles{}, broadcaster, Subscription{
			Query:     Query{Cursor: new(spaces.EventSeq(0))},
			Reader:    SpaceCredentialReader{Space: spaceB},
			ExpiresAt: time.Now().Add(time.Minute),
		})
		require.Equal(t, []string{"commit 2 2"}, summary(conn.take()))

		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, []string{"error ExpiredToken"}, summary(conn.take()))
		require.NoError(t, stop(), "expiry ends the stream cleanly")
	})
}

func TestStreamSpaceDeletions(t *testing.T) {
	tests := []struct {
		name   string
		reader Reader
		filter []habitat_syntax.SpaceURI
		want   []string
	}{
		{
			name:   "user receives deletions of spaces it owns",
			reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
			want:   []string{"deleted 3 " + string(spaceC.AtSpace())},
		},
		{
			name:   "user receives deletions of spaces it names",
			reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
			filter: []habitat_syntax.SpaceURI{spaceA, spaceB},
			want: []string{
				"deleted 1 " + string(spaceA.AtSpace()),
				"deleted 2 " + string(spaceB.AtSpace()),
			},
		},
		{
			name:   "space credential receives its space's deletion",
			reader: SpaceCredentialReader{Space: spaceB},
			want:   []string{"deleted 2 " + string(spaceB.AtSpace())},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := &fakeLog{}
				log.deleteSpace(spaceA)
				log.deleteSpace(spaceB)
				log.deleteSpace(spaceC)
				conn, stop := setupStream(t, log, &fakeRoles{}, NewBroadcaster(), Subscription{
					Query: Query{
						Cursor: new(spaces.EventSeq(0)),
						Filter: spaces.EventFilter{Spaces: tt.filter},
					},
					Reader: tt.reader,
				})
				require.Equal(t, tt.want, summary(conn.take()))
				require.ErrorIs(t, stop(), context.Canceled)
			})
		})
	}
}

func TestPrepareNarrowsSpaceCredential(t *testing.T) {
	sub, err := Prepare(Subscription{Reader: SpaceCredentialReader{Space: spaceA}})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{spaceA}, sub.Query.Filter.Spaces)

	sub, err = Prepare(Subscription{
		Query: Query{
			Filter: spaces.EventFilter{Spaces: []habitat_syntax.SpaceURI{spaceB, spaceA}},
		},
		Reader: SpaceCredentialReader{Space: spaceA},
	})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{spaceA}, sub.Query.Filter.Spaces)

	_, err = Prepare(Subscription{
		Query:  Query{Filter: spaces.EventFilter{Spaces: []habitat_syntax.SpaceURI{spaceB}}},
		Reader: SpaceCredentialReader{Space: spaceA},
	})
	require.ErrorIs(t, err, ErrCredentialMismatch)

	// A user's filter is left as asked.
	sub, err = Prepare(Subscription{
		Query:  Query{Filter: spaces.EventFilter{Spaces: []habitat_syntax.SpaceURI{spaceB}}},
		Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
	})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{spaceB}, sub.Query.Filter.Spaces)
}

func TestStreamLimitsUserToScope(t *testing.T) {
	otherType := habitat_syntax.ConstructSpaceURI(org, "network.habitat.other", "d")
	tests := []struct {
		name   string
		scopes []string
		want   []string
	}{
		{
			name:   "no space scopes read nothing",
			scopes: []string{"atproto", "transition:generic"},
			want:   nil,
		},
		{
			name:   "a type-limited grant reads only that type",
			scopes: []string{"space:network.habitat.group?authority=*&action=read"},
			want:   []string{"commit 1 1", "commit 2 2"},
		},
		{
			name:   "read_self reads only the user's own records",
			scopes: []string{"space:*?authority=*&action=read_self"},
			want:   []string{"commit 1 1", "commit 3 3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := &fakeLog{}
				log.add(spaceA, "1")
				log.addBy(spaceA, "2", "did:plc:bob")
				log.add(otherType, "3")
				roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{
					spaceA: true, otherType: true,
				}}
				conn, stop := setupStream(t, log, roles, NewBroadcaster(), Subscription{
					Query: Query{Cursor: new(spaces.EventSeq(0))},
					Reader: UserReader{
						DID:   alice,
						Scope: spacescope.FromScopes(tt.scopes, alice),
					},
				})
				got := summary(conn.take())
				if tt.want == nil {
					require.Empty(t, got)
				} else {
					require.Equal(t, tt.want, got)
				}
				require.ErrorIs(t, stop(), context.Canceled)
			})
		})
	}
}
