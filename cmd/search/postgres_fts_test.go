package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupPostgresFTSIndex(t *testing.T) *postgresFTSIndex {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("search"),
		postgres.WithUsername("search"),
		postgres.WithPassword("search"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	require.NoError(t, err)

	index, err := newPostgresFTSIndex(db)
	require.NoError(t, err)
	return index
}

func doc(uri, orgDID, content string) Document {
	return Document{
		URI:        uri,
		SpaceURI:   "ats://" + orgDID + "/app.space/skey1",
		OrgDID:     orgDID,
		RecordType: "app.note",
		Content:    content,
		UpdatedAt:  time.Now(),
	}
}

func TestPostgresFTSIndex_UpsertAndQuery(t *testing.T) {
	index := setupPostgresFTSIndex(t)
	ctx := context.Background()

	require.NoError(t, index.Upsert(ctx, doc(
		"ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey1",
		"did:plc:org1",
		"the quarterly budget review notes",
	)))
	require.NoError(t, index.Upsert(ctx, doc(
		"ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey2",
		"did:plc:org1",
		"unrelated grocery list",
	)))

	result, err := index.Query(ctx, QueryParams{OrgDID: "did:plc:org1", QueryText: "budget", Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey1", result.Results[0].URI)
}

func TestPostgresFTSIndex_QueryFiltersByOrg(t *testing.T) {
	index := setupPostgresFTSIndex(t)
	ctx := context.Background()

	require.NoError(t, index.Upsert(ctx, doc(
		"ats://did:plc:org2/app.space/skey1/did:plc:user1/app.note/rkey1",
		"did:plc:org2",
		"budget notes for org2",
	)))

	result, err := index.Query(ctx, QueryParams{OrgDID: "did:plc:org1", QueryText: "budget", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}

func TestPostgresFTSIndex_Delete(t *testing.T) {
	index := setupPostgresFTSIndex(t)
	ctx := context.Background()

	d := doc("ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey1", "did:plc:org1", "budget notes")
	require.NoError(t, index.Upsert(ctx, d))
	require.NoError(t, index.Delete(ctx, d.URI))

	result, err := index.Query(ctx, QueryParams{OrgDID: "did:plc:org1", QueryText: "budget", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}

func TestPostgresFTSIndex_QueryRespectsLimitAndCursor(t *testing.T) {
	index := setupPostgresFTSIndex(t)
	ctx := context.Background()

	for i := range 3 {
		require.NoError(t, index.Upsert(ctx, doc(
			"ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey"+string(rune('a'+i)),
			"did:plc:org1",
			"budget notes page",
		)))
	}

	first, err := index.Query(ctx, QueryParams{OrgDID: "did:plc:org1", QueryText: "budget", Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Results, 2)
	require.NotEmpty(t, first.NextCursor)

	second, err := index.Query(ctx, QueryParams{
		OrgDID: "did:plc:org1", QueryText: "budget", Limit: 2, Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, second.Results, 1)
}
