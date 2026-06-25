package relationship

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Store manages relationship tuples and groups as org-owned space records,
// mirrored into the fgastore graph for resolution.
type Store interface {
	WriteTuple(
		ctx context.Context,
		subject Subject,
		relation Role,
		object Object,
	) (habitat_syntax.SpaceRecordURI, error)
	DeleteTuple(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error
	ListTuples(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		filter TupleFilter,
	) ([]Tuple, error)

	Check(
		ctx context.Context,
		did syntax.DID,
		relation Role,
		space habitat_syntax.SpaceURI,
	) (bool, error)
	ListSubjects(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		relation Role,
	) ([]syntax.DID, error)
	ListObjects(
		ctx context.Context,
		did syntax.DID,
		relation Role,
	) ([]habitat_syntax.SpaceURI, error)

	CreateGroup(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		name, description string,
	) (habitat_syntax.SpaceRecordURI, error)
	DeleteGroup(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error
	ListGroups(ctx context.Context, space habitat_syntax.SpaceURI) ([]Group, error)
}

// TupleFilter narrows ListTuples results. Empty fields match anything.
type TupleFilter struct {
	ObjectURI  string // matches a SpaceObject.Space or GroupObject.Group
	SubjectDID string // matches a UserSubject.DID
	Relation   string
}

type store struct {
	spaces spaces.Store
	fga    fgastore.Store
}

var _ Store = (*store)(nil)

// NewStore constructs a relationship Store over a spaces store and the fga graph.
func NewStore(spaceStore spaces.Store, fga fgastore.Store) *store {
	return &store{spaces: spaceStore, fga: fga}
}

var (
	tupleNSID = syntax.NSID(habitat_syntax.ReservedRelationshipTupleNSID)
	groupNSID = syntax.NSID(habitat_syntax.ReservedRelationshipGroupNSID)
)

// tupleRkey derives a deterministic record key from the FGA triple so a tuple
// maps 1:1 to its record (writes are idempotent, deletes are addressable).
func tupleRkey(user, relation, object string) syntax.RecordKey {
	sum := sha256.Sum256([]byte(user + "\x00" + relation + "\x00" + object))
	return syntax.RecordKey(hex.EncodeToString(sum[:]))
}

func (s *store) WriteTuple(
	ctx context.Context,
	subject Subject,
	relation Role,
	object Object,
) (habitat_syntax.SpaceRecordURI, error) {
	user, err := subjectToFGAUser(subject)
	if err != nil {
		return "", err
	}
	objectKey, fgaRelation, err := resolveObject(relation, object)
	if err != nil {
		return "", err
	}
	space, err := governingSpace(object)
	if err != nil {
		return "", err
	}

	value := map[string]any{
		"subject":   subjectValue(subject),
		"relation":  string(relation),
		"object":    objectValue(object),
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	rkey := tupleRkey(user, fgaRelation, objectKey)
	uri, _, err := s.spaces.PutRecord(ctx, space, space.SpaceOwner(), tupleNSID, rkey, value)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return "", err
	} else if err != nil {
		return "", fmt.Errorf("write tuple record: %w", err)
	}

	if err := s.writeFGA(ctx, user, fgaRelation, objectKey); err != nil {
		return "", err
	}
	return uri, nil
}

func (s *store) DeleteTuple(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	_, space, repo, collection, rkey, err := habitat_syntax.ParseSpaceRecordURI(uri.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTupleNotFound, err)
	}
	if collection.String() != habitat_syntax.ReservedRelationshipTupleNSID {
		return fmt.Errorf("%w: not a tuple record", ErrTupleNotFound)
	}

	rec, err := s.spaces.GetRecord(ctx, space, repo, collection, rkey)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrTupleNotFound
	} else if err != nil {
		return fmt.Errorf("get tuple record: %w", err)
	}

	tup, err := decodeTuple(uri, rec.Value)
	if err != nil {
		return err
	}
	user, err := subjectToFGAUser(tup.Subject)
	if err != nil {
		return err
	}
	objectKey, fgaRelation, err := resolveObject(tup.Relation, tup.Object)
	if err != nil {
		return err
	}
	if err := s.deleteFGA(ctx, user, fgaRelation, objectKey); err != nil {
		return err
	}
	return s.spaces.DeleteRecord(ctx, space, collection, rkey.String())
}

func (s *store) ListTuples(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	filter TupleFilter,
) ([]Tuple, error) {
	records, err := s.spaces.ListRecords(ctx, space, space.SpaceOwner(), &tupleNSID)
	if err != nil {
		return nil, fmt.Errorf("list tuple records: %w", err)
	}
	tuples := make([]Tuple, 0, len(records))
	for _, rec := range records {
		uri := habitat_syntax.ConstructSpaceRecordURI(
			space,
			space.SpaceOwner(),
			tupleNSID,
			rec.Rkey,
		)
		tup, err := decodeTuple(uri, rec.Value)
		if err != nil {
			return nil, err
		}
		if !filter.matches(tup) {
			continue
		}
		tuples = append(tuples, tup)
	}
	return tuples, nil
}

func (s *store) Check(
	ctx context.Context,
	did syntax.DID,
	relation Role,
	space habitat_syntax.SpaceURI,
) (bool, error) {
	fgaRelation, err := relation.fgaSpaceRelation()
	if err != nil {
		return false, err
	}
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelation,
		fgastore.SpaceObjectKey(space),
		ownerContextualTuple(space),
	)
}

func (s *store) ListSubjects(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	relation Role,
) ([]syntax.DID, error) {
	fgaRelation, err := relation.fgaSpaceRelation()
	if err != nil {
		return nil, err
	}
	users, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(space),
		fgaRelation,
		ownerContextualTuple(space),
	)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	dids := make([]syntax.DID, 0, len(users))
	for _, u := range users {
		did, err := fgastore.MemberUserToDID(u)
		if err != nil {
			// Skip non-user results (the model only expands to users today).
			continue
		}
		dids = append(dids, did)
	}
	return dids, nil
}

func (s *store) ListObjects(
	ctx context.Context,
	did syntax.DID,
	relation Role,
) ([]habitat_syntax.SpaceURI, error) {
	fgaRelation, err := relation.fgaSpaceRelation()
	if err != nil {
		return nil, err
	}
	objects, err := s.fga.ListObjects(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelation,
		fgastore.TypeSpace,
	)
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	spaceURIs := make([]habitat_syntax.SpaceURI, 0, len(objects))
	for _, o := range objects {
		uri, err := fgastore.ParseSpaceObjectKey(o)
		if err != nil {
			continue
		}
		spaceURIs = append(spaceURIs, uri)
	}
	return spaceURIs, nil
}

func (s *store) CreateGroup(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	name, description string,
) (habitat_syntax.SpaceRecordURI, error) {
	value := map[string]any{
		"name":      name,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if description != "" {
		value["description"] = description
	}
	uri, _, err := s.spaces.PutRecord(ctx, space, space.SpaceOwner(), groupNSID, "", value)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return "", err
	} else if err != nil {
		return "", fmt.Errorf("create group record: %w", err)
	}
	return uri, nil
}

func (s *store) ListGroups(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]Group, error) {
	records, err := s.spaces.ListRecords(ctx, space, space.SpaceOwner(), &groupNSID)
	if err != nil {
		return nil, fmt.Errorf("list group records: %w", err)
	}
	groups := make([]Group, 0, len(records))
	for _, rec := range records {
		uri := habitat_syntax.ConstructSpaceRecordURI(
			space,
			space.SpaceOwner(),
			groupNSID,
			rec.Rkey,
		)
		groups = append(groups, decodeGroup(uri, rec.Value))
	}
	return groups, nil
}

func (s *store) DeleteGroup(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	_, space, repo, collection, rkey, err := habitat_syntax.ParseSpaceRecordURI(uri.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGroupNotFound, err)
	}
	if collection.String() != habitat_syntax.ReservedRelationshipGroupNSID {
		return fmt.Errorf("%w: not a group record", ErrGroupNotFound)
	}
	if _, err := s.spaces.GetRecord(ctx, space, repo, collection, rkey); errors.Is(
		err,
		spaces.ErrRecordNotFound,
	) {
		return ErrGroupNotFound
	} else if err != nil {
		return fmt.Errorf("get group record: %w", err)
	}

	// A group's membership tuples (object = group) and any same-space tuples that
	// reference the group as a subject all live in the group's governing space, so
	// we can enumerate and delete them via the tuple records. Cross-space tuples
	// that use the group as a subject must be removed by the caller.
	tuples, err := s.ListTuples(ctx, space, TupleFilter{})
	if err != nil {
		return err
	}
	for _, t := range tuples {
		if referencesGroup(t, uri) {
			if err := s.DeleteTuple(ctx, t.URI); err != nil {
				return err
			}
		}
	}

	return s.spaces.DeleteRecord(ctx, space, collection, rkey.String())
}

// referencesGroup reports whether a tuple has the given group as its object or
// as its subject.
func referencesGroup(t Tuple, group habitat_syntax.SpaceRecordURI) bool {
	if o, ok := t.Object.(GroupObject); ok && o.Group == group {
		return true
	}
	if s, ok := t.Subject.(GroupSubject); ok && s.Group == group {
		return true
	}
	return false
}

// writeFGA idempotently writes a relationship tuple into the graph.
func (s *store) writeFGA(ctx context.Context, user, relation, object string) error {
	if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{tuple.NewTupleKey(object, relation, user)},
			OnDuplicate: "ignore",
		},
	}); err != nil {
		return fmt.Errorf("write fga tuple: %w", err)
	}
	return nil
}

// deleteFGA idempotently removes a relationship tuple from the graph.
func (s *store) deleteFGA(ctx context.Context, user, relation, object string) error {
	if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(
					tuple.NewTupleKey(object, relation, user),
				),
			},
			OnMissing: "ignore",
		},
	}); err != nil {
		return fmt.Errorf("delete fga tuple: %w", err)
	}
	return nil
}

func (f TupleFilter) matches(t Tuple) bool {
	if f.Relation != "" && string(t.Relation) != f.Relation {
		return false
	}
	if f.ObjectURI != "" && objectURIString(t.Object) != f.ObjectURI {
		return false
	}
	if f.SubjectDID != "" {
		user, ok := t.Subject.(UserSubject)
		if !ok || user.DID.String() != f.SubjectDID {
			return false
		}
	}
	return true
}

func objectURIString(o Object) string {
	switch o := o.(type) {
	case SpaceObject:
		return o.Space.String()
	case GroupObject:
		return o.Group.String()
	default:
		return ""
	}
}

func decodeTuple(uri habitat_syntax.SpaceRecordURI, value map[string]any) (Tuple, error) {
	subject, err := ParseSubject(value["subject"])
	if err != nil {
		return Tuple{}, err
	}
	object, err := ParseObject(value["object"])
	if err != nil {
		return Tuple{}, err
	}
	relation, _ := value["relation"].(string)
	return Tuple{
		URI:      uri,
		Subject:  subject,
		Relation: Role(relation),
		Object:   object,
	}, nil
}

func decodeGroup(uri habitat_syntax.SpaceRecordURI, value map[string]any) Group {
	name, _ := value["name"].(string)
	description, _ := value["description"].(string)
	createdAt, _ := value["createdAt"].(string)
	return Group{
		URI:         uri,
		Name:        name,
		Description: description,
		CreatedAt:   createdAt,
	}
}
