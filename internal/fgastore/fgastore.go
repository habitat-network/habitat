package fgastore

import (
	"context"
	"errors"
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/tuple"
)

type Store interface {
	Check(ctx context.Context, user, relation, object string) (bool, error)
	Write(ctx context.Context, user, relation, object string) error
	Delete(ctx context.Context, user, relation, object string) error
	Close() error
}

type FGA struct {
	ds      storage.OpenFGADatastore
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
	store, err := ds.CreateStore(ctx, &openfgav1.Store{Name: "habitat"})
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}
	if err := ds.WriteAuthorizationModel(ctx, store.GetId(), authModel()); err != nil {
		return nil, fmt.Errorf("write auth model: %w", err)
	}
	return &FGA{ds: ds, storeID: store.GetId()}, nil
}

func (f *FGA) Check(ctx context.Context, user, relation, object string) (bool, error) {
	_, err := f.ds.ReadUserTuple(ctx, f.storeID, storage.ReadUserTupleFilter{
		User:     user,
		Relation: relation,
		Object:   object,
	}, storage.ReadUserTupleOptions{})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("check: %w", err)
	}
	return true, nil
}

func (f *FGA) Write(ctx context.Context, user, relation, object string) error {
	tk := tuple.NewTupleKey(object, relation, user)
	return f.ds.Write(ctx, f.storeID,
		storage.Deletes{},
		storage.Writes{tk},
	)
}

func (f *FGA) Delete(ctx context.Context, user, relation, object string) error {
	tk := tuple.NewTupleKey(object, relation, user)
	tkWithoutCond := tuple.TupleKeyToTupleKeyWithoutCondition(tk)
	return f.ds.Write(ctx, f.storeID,
		storage.Deletes{tkWithoutCond},
		storage.Writes{},
	)
}

func (f *FGA) Close() error {
	f.ds.Close()
	return nil
}
