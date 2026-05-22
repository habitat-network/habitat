package fgastore

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/assets"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/storage/sqlite"
	"github.com/openfga/openfga/pkg/tuple"
)

var _ Store = (*FGA)(nil)

// Tuple represents a relationship tuple (used as contextual tuples in requests).
type Tuple struct {
	User     string
	Relation string
	Object   string
}

type Store interface {
	Check(
		ctx context.Context,
		user, relation, object string,
		contextualTuples ...Tuple,
	) (bool, error)
	Write(ctx context.Context, user, relation, object string) error
	Delete(ctx context.Context, user, relation, object string) error
	ListObjects(
		ctx context.Context,
		user, relation, objectType string,
		contextualTuples ...Tuple,
	) ([]string, error)
	ListUsers(
		ctx context.Context,
		object, relation string,
		contextualTuples ...Tuple,
	) ([]string, error)
	Close() error
}

type FGA struct {
	ds      storage.OpenFGADatastore
	svr     *server.Server
	storeID string
}

func NewPostgres(ctx context.Context, uri string) (*FGA, error) {
	db, err := goose.OpenDBWithDriver("pgx", uri)
	if err != nil {
		return nil, fmt.Errorf("fgastore open db: %w", err)
	}
	// postgres.New below will open its own connection
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn().Err(err).Msg("fgastore close db")
		}
	}()
	pgFS, err := fs.Sub(assets.EmbedMigrations, assets.PostgresMigrationDir)
	if err != nil {
		return nil, fmt.Errorf("fgastore migration fs: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		pgFS,
		goose.WithTableName("fga_goose_db_version"),
	)
	if err != nil {
		return nil, fmt.Errorf("fgastore migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return nil, fmt.Errorf("fgastore postgres migrations: %w", err)
	}
	ds, err := postgres.New(uri, &sqlcommon.Config{
		MaxTuplesPerWriteField: storage.DefaultMaxTuplesPerWrite,
		MaxTypesPerModelField:  storage.DefaultMaxTypesPerAuthorizationModel,
	})
	if err != nil {
		return nil, fmt.Errorf("fgastore postgres: %w", err)
	}
	return newFromDS(ctx, ds)
}

func NewSQLite(ctx context.Context, uri string) (*FGA, error) {
	preparedURI, err := sqlite.PrepareDSN(uri)
	if err != nil {
		return nil, fmt.Errorf("fgastore prepare dsn: %w", err)
	}
	db, err := goose.OpenDBWithDriver("sqlite", preparedURI)
	if err != nil {
		return nil, fmt.Errorf("fgastore open db: %w", err)
	}
	// sqlite.New below will open its own connection
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn().Err(err).Msg("fgastore close db")
		}
	}()
	sqliteFS, err := fs.Sub(assets.EmbedMigrations, assets.SqliteMigrationDir)
	if err != nil {
		return nil, fmt.Errorf("fgastore migration fs: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		sqliteFS,
		goose.WithTableName("fga_goose_db_version"),
	)
	if err != nil {
		return nil, fmt.Errorf("fgastore migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return nil, fmt.Errorf("fgastore sqlite migrations: %w", err)
	}
	ds, err := sqlite.New(uri, &sqlcommon.Config{
		MaxTuplesPerWriteField: storage.DefaultMaxTuplesPerWrite,
		MaxTypesPerModelField:  storage.DefaultMaxTypesPerAuthorizationModel,
	})
	if err != nil {
		return nil, fmt.Errorf("fgastore sqlite: %w", err)
	}
	return newFromDS(ctx, ds)
}

func newFromDS(ctx context.Context, ds storage.OpenFGADatastore) (*FGA, error) {
	svr, err := server.NewServerWithOpts(
		server.WithDatastore(ds),
		server.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("new server: %w", err)
	}
	createResp, err := svr.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: "habitat"})
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}
	model := authModel()
	if _, err := svr.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         createResp.GetId(),
		SchemaVersion:   model.GetSchemaVersion(),
		TypeDefinitions: model.GetTypeDefinitions(),
	}); err != nil {
		return nil, fmt.Errorf("write auth model: %w", err)
	}
	return &FGA{ds: ds, svr: svr, storeID: createResp.GetId()}, nil
}

func (f *FGA) Check(
	ctx context.Context,
	user, relation, object string,
	contextualTuples ...Tuple,
) (bool, error) {
	resp, err := f.svr.Check(ctx, &openfgav1.CheckRequest{
		StoreId: f.storeID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			User:     user,
			Relation: relation,
			Object:   object,
		},
		ContextualTuples: &openfgav1.ContextualTupleKeys{
			TupleKeys: toTupleKeys(contextualTuples),
		},
	})
	if err != nil {
		return false, fmt.Errorf("check: %w", err)
	}
	return resp.GetAllowed(), nil
}

func (f *FGA) ListObjects(
	ctx context.Context,
	user, relation, objectType string,
	contextualTuples ...Tuple,
) ([]string, error) {
	resp, err := f.svr.ListObjects(ctx, &openfgav1.ListObjectsRequest{
		StoreId:  f.storeID,
		User:     user,
		Relation: relation,
		Type:     objectType,
		ContextualTuples: &openfgav1.ContextualTupleKeys{
			TupleKeys: toTupleKeys(contextualTuples),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	return resp.GetObjects(), nil
}

func (f *FGA) ListUsers(
	ctx context.Context,
	object, relation string,
	contextualTuples ...Tuple,
) ([]string, error) {
	objType, objID, ok := strings.Cut(object, ":")
	if !ok {
		return nil, fmt.Errorf("invalid object format %q: expected type:id", object)
	}
	resp, err := f.svr.ListUsers(ctx, &openfgav1.ListUsersRequest{
		StoreId:  f.storeID,
		Object:   &openfgav1.Object{Type: objType, Id: objID},
		Relation: relation,
		UserFilters: []*openfgav1.UserTypeFilter{
			{Type: "user"},
		},
		ContextualTuples: toTupleKeys(contextualTuples),
	})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]string, len(resp.GetUsers()))
	for i, u := range resp.GetUsers() {
		if obj := u.GetObject(); obj != nil {
			users[i] = obj.GetType() + ":" + obj.GetId()
		}
	}
	return users, nil
}

func (f *FGA) Write(ctx context.Context, user, relation, object string) error {
	_, err := f.svr.Write(ctx, &openfgav1.WriteRequest{
		StoreId: f.storeID,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: []*openfgav1.TupleKey{tuple.NewTupleKey(object, relation, user)},
		},
	})
	return err
}

func (f *FGA) Delete(ctx context.Context, user, relation, object string) error {
	_, err := f.svr.Write(ctx, &openfgav1.WriteRequest{
		StoreId: f.storeID,
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(object, relation, user)),
			},
		},
	})
	return err
}

func (f *FGA) Close() error {
	f.svr.Close()
	f.ds.Close()
	return nil
}

// toTupleKeys converts domain Tuples to OpenFGA TupleKey pointers.
func toTupleKeys(tuples []Tuple) []*openfgav1.TupleKey {
	if len(tuples) == 0 {
		return nil
	}
	keys := make([]*openfgav1.TupleKey, len(tuples))
	for i, ct := range tuples {
		keys[i] = tuple.NewTupleKey(ct.Object, ct.Relation, ct.User)
	}
	return keys
}
