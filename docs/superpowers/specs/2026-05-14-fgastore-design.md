# fgastore: OpenFGA-backed Authorization Store

**Goal:** Define an authorization model (user, record, org, role as object types) and expose a `Store` interface wrapping OpenFGA's storage layer, usable by the AuthZEN evaluator.

**Architecture:** A new `internal/fgastore/` package wraps OpenFGA's `storage.OpenFGADatastore` behind a habitat-specific interface. The constructor picks the backend (PostgreSQL for production, in-memory for tests). The auth model is defined as OpenFGA `TypeDefinition` structs at startup. Check/Write/Delete delegate to the storage layer's direct tuple operations.

**Tech Stack:** Go, OpenFGA `pkg/storage` + `pkg/storage/postgres`, OpenFGA protobuf API types

---

## Types & Relations

Four types with direct-only relations (`[user]` type restrictions — no usersets or computed relations yet):

| Type | Relations | Description |
|------|-----------|-------------|
| `user` | _(none)_ | A DID (e.g. `user:did:plc:abc123`) |
| `record` | `owner`, `can_read` | An AT URI record |
| `org` | `member`, `admin` | An organization |
| `role` | `assigned` | A named role _(defined but not yet wired into records/orgs)_ |

## Package Structure

```
internal/fgastore/
  fgastore.go       — Store interface + FGA struct implementing it
  model.go           — Authorization model as Go protobuf structs
  fgastore_test.go   — Tests with in-memory backend
```

## Store Interface

```go
type Store interface {
    Check(ctx context.Context, user, relation, object string) (bool, error)
    Write(ctx context.Context, user, relation, object string) error
    Delete(ctx context.Context, user, relation, object string) error
    Close() error
}
```

All parameters are plain strings. Callers construct values like:
- `user`: `"user:did:plc:abc123"` or `"user:*"` (future: `"org:uuid#member"`)
- `relation`: `"can_read"`, `"owner"`, `"member"`, `"admin"`, `"assigned"`
- `object`: `"record:did:plc:abc123/net.habitat.post/3kq..."`, `"org:uuid"`, `"role:admin"`

## Implementation (`fgastore.go`)

### FGA struct

```go
type FGA struct {
    ds      storage.OpenFGADatastore
    storeID string
    modelID string
}
```

### Constructors

```go
// NewPostgres creates an fgastore backed by OpenFGA's PostgreSQL storage.
// Caller should call Close() when done.
func NewPostgres(ctx context.Context, uri string) (*FGA, error) {
    ds, err := postgres.New(uri, &sqlcommon.Config{
        MaxTuplesPerWrite: 100,
        MaxTypesPerAuthorizationModel: 100,
    })
    if err != nil {
        return nil, fmt.Errorf("postgres: %w", err)
    }
    return newFromDS(ctx, ds)
}

// NewInMemory creates an fgastore backed by OpenFGA's in-memory storage (for tests).
func NewInMemory(ctx context.Context) (*FGA, error) {
    return newFromDS(ctx, memory.New())
}

func newFromDS(ctx context.Context, ds storage.OpenFGADatastore) (*FGA, error) {
    store, err := ds.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: "habitat"})
    if err != nil {
        return nil, fmt.Errorf("create store: %w", err)
    }
    if err := ds.WriteAuthorizationModel(ctx, store.GetId(), authModel()); err != nil {
        return nil, fmt.Errorf("write auth model: %w", err)
    }
    latest, err := ds.FindLatestAuthorizationModel(ctx, store.GetId())
    if err != nil {
        return nil, fmt.Errorf("find latest model: %w", err)
    }
    return &FGA{
        ds:      ds,
        storeID: store.GetId(),
        modelID: latest.GetId(),
    }, nil
}
```

### Methods

```go
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
        return false, fmt.Errorf("read tuple: %w", err)
    }
    return true, nil
}

func (f *FGA) Write(ctx context.Context, user, relation, object string) error {
    tk := &openfgav1.TupleKey{User: user, Relation: relation, Object: object}
    return f.ds.Write(ctx, f.storeID, storage.Deletes{}, storage.Writes{TupleKeys: []*openfgav1.TupleKey{tk}})
}

func (f *FGA) Delete(ctx context.Context, user, relation, object string) error {
    tk := &openfgav1.TupleKey{User: user, Relation: relation, Object: object}
    return f.ds.Write(ctx, f.storeID, storage.Deletes{TupleKeys: []*openfgav1.TupleKey{tk}}, storage.Writes{})
}

func (f *FGA) Close() error {
    return f.ds.Close()
}
```

## Authorization Model (`model.go`)

Defined as a function returning `*openfgav1.AuthorizationModel`:

```go
func authModel() *openfgav1.AuthorizationModel {
    return &openfgav1.AuthorizationModel{
        SchemaVersion: "1.1",
        TypeDefinitions: []*openfgav1.TypeDefinition{
            {Type: "user"},
            {
                Type: "record",
                Relations: map[string]*openfgav1.Userset{
                    "owner":    {Userset: &openfgav1.Userset_This{}},
                    "can_read": {Userset: &openfgav1.Userset_This{}},
                },
                Metadata: &openfgav1.Metadata{
                    Relations: map[string]*openfgav1.RelationMetadata{
                        "owner": {
                            DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
                                {Type: "user"},
                            },
                        },
                        "can_read": {
                            DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
                                {Type: "user"},
                            },
                        },
                    },
                },
            },
            {
                Type: "org",
                Relations: map[string]*openfgav1.Userset{
                    "member": {Userset: &openfgav1.Userset_This{}},
                    "admin":  {Userset: &openfgav1.Userset_This{}},
                },
                Metadata: &openfgav1.Metadata{
                    Relations: map[string]*openfgav1.RelationMetadata{
                        "member": {
                            DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
                                {Type: "user"},
                            },
                        },
                        "admin": {
                            DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
                                {Type: "user"},
                            },
                        },
                    },
                },
            },
            {
                Type: "role",
                Relations: map[string]*openfgav1.Userset{
                    "assigned": {Userset: &openfgav1.Userset_This{}},
                },
                Metadata: &openfgav1.Metadata{
                    Relations: map[string]*openfgav1.RelationMetadata{
                        "assigned": {
                            DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
                                {Type: "user"},
                            },
                        },
                    },
                },
            },
        },
    }
}
```

## Tests (`fgastore_test.go`)

1. **NewInMemory creates a usable store** — smoke test
2. **Check returns true for existing tuple** — Write + Check
3. **Check returns false for non-existent tuple** — no Write, just Check
4. **Check returns false after Delete** — Write + Check + Delete + Check
5. **Check returns false for different relation** — Write `can_read`, Check `owner`
6. **Check returns false for different user** — Write for userA, Check for userB

All use `NewInMemory` — no external database needed.

## Out of Scope

- Wiring into `cmd/pear/main.go`
- Replacing the existing `internal/permissions/store.go`
- Adding relationship tuples from permission management endpoints
- Role-to-record or org-to-record relation wiring
- AuthZEN evaluator integration
- Migration strategy for existing permission tuples

## Dependency

- `github.com/openfga/openfga` (provides `pkg/storage`, `pkg/storage/postgres`, `pkg/storage/memory`)
- Transitive: OpenFGA API protobuf types (`github.com/openfga/api/proto/openfga/v1`)
