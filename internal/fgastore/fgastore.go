package fgastore

import (
	"context"
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
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
	ds, err := postgres.New(uri, &sqlcommon.Config{})
	if err != nil {
		return nil, fmt.Errorf("fgastore postgres: %w", err)
	}
	return newFromDS(ctx, ds)
}

func NewInMemory(ctx context.Context) (*FGA, error) {
	return newFromDS(ctx, memory.New())
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
	req := &openfgav1.CheckRequest{
		StoreId: f.storeID,
		TupleKey: &openfgav1.CheckRequestTupleKey{
			User:     user,
			Relation: relation,
			Object:   object,
		},
	}
	if len(contextualTuples) > 0 {
		req.ContextualTuples = &openfgav1.ContextualTupleKeys{
			TupleKeys: make([]*openfgav1.TupleKey, len(contextualTuples)),
		}
		for i, ct := range contextualTuples {
			req.ContextualTuples.TupleKeys[i] = tuple.NewTupleKey(ct.Object, ct.Relation, ct.User)
		}
	}
	resp, err := f.svr.Check(ctx, req)
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
	req := &openfgav1.ListObjectsRequest{
		StoreId:  f.storeID,
		User:     user,
		Relation: relation,
		Type:     objectType,
	}
	if len(contextualTuples) > 0 {
		req.ContextualTuples = &openfgav1.ContextualTupleKeys{
			TupleKeys: make([]*openfgav1.TupleKey, len(contextualTuples)),
		}
		for i, ct := range contextualTuples {
			req.ContextualTuples.TupleKeys[i] = tuple.NewTupleKey(ct.Object, ct.Relation, ct.User)
		}
	}
	resp, err := f.svr.ListObjects(ctx, req)
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
	req := &openfgav1.ListUsersRequest{
		StoreId:  f.storeID,
		Object:   &openfgav1.Object{Type: objType, Id: objID},
		Relation: relation,
		UserFilters: []*openfgav1.UserTypeFilter{
			{Type: "user"},
		},
	}
	if len(contextualTuples) > 0 {
		req.ContextualTuples = make([]*openfgav1.TupleKey, len(contextualTuples))
		for i, ct := range contextualTuples {
			req.ContextualTuples[i] = tuple.NewTupleKey(ct.Object, ct.Relation, ct.User)
		}
	}
	resp, err := f.svr.ListUsers(ctx, req)
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
	tk := tuple.NewTupleKey(object, relation, user)
	_, err := f.svr.Write(ctx, &openfgav1.WriteRequest{
		StoreId: f.storeID,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: []*openfgav1.TupleKey{tk},
		},
	})
	return err
}

func (f *FGA) Delete(ctx context.Context, user, relation, object string) error {
	tk := tuple.NewTupleKey(object, relation, user)
	tkWithoutCond := tuple.TupleKeyToTupleKeyWithoutCondition(tk)
	_, err := f.svr.Write(ctx, &openfgav1.WriteRequest{
		StoreId: f.storeID,
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{tkWithoutCond},
		},
	})
	return err
}

func (f *FGA) Close() error {
	f.svr.Close()
	f.ds.Close()
	return nil
}
