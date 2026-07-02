package relationship

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// tupleCollection is the reserved collection holding tuple records within the
// governing space.
var tupleCollection = syntax.NSID(habitat_syntax.ReservedRelationshipTupleNSID)

// TupleView is a decoded relationship tuple together with its record URI.
type TupleView struct {
	URI      habitat_syntax.SpaceRecordURI
	Subject  Subject
	Relation Role
	Object   habitat_syntax.SpaceURI
}

// ListTuplesFilter narrows ListTuples results. Nil/empty fields are ignored.
type ListTuplesFilter struct {
	Object      *habitat_syntax.SpaceURI
	SubjectDID  *syntax.DID
	SubjectKind SubjectKind
	Relation    *Role
}

// Store mirrors relationship tuples into the governing space's AT Protocol
// records (org-owned) and the OpenFGA graph. The two writes are wrapped in a DB
// transaction (via spaces.Store.WithTx) so an FGA failure rolls back the record.
type Store struct {
	db     *gorm.DB
	spaces spaces.Store
	fga    fgastore.Store
}

func NewStore(database *gorm.DB, spacesStore spaces.Store, fga fgastore.Store) *Store {
	return &Store{db: database, spaces: spacesStore, fga: fga}
}

// ownerContextualTuple makes the space owner (the org) a recognized owner of
// the space without storing the tuple in FGA, mirroring internal/spaces.
func ownerContextualTuple(uri habitat_syntax.SpaceURI) fgastore.Tuple {
	return fgastore.Tuple{
		User:     fgastore.MemberUserString(uri.SpaceOwner()),
		Relation: fgastore.RelationSpaceOwner,
		Object:   fgastore.SpaceObjectKey(uri),
	}
}

// WriteTuple writes a relationship tuple, creating it if it does not already
// exist. The tuple record is stored org-owned (repo = space owner) within the
// object space, and the relationship is mirrored into FGA. The object space is
// the governing space. Idempotent: an identical existing tuple is returned
// unchanged.
func (s *Store) WriteTuple(
	ctx context.Context,
	subject Subject,
	relation Role,
	object habitat_syntax.SpaceURI,
) (habitat_syntax.SpaceRecordURI, error) {
	fgaRelation, err := roleToFGARelation(relation)
	if err != nil {
		return "", err
	}
	fgaUser, err := subject.fgaUserString()
	if err != nil {
		return "", err
	}

	// Idempotency: if an identical tuple record already exists, reuse it so we
	// don't accumulate duplicate records that map to a single FGA tuple (which
	// would desync on delete).
	existing, err := s.listTuples(ctx, object)
	if err != nil {
		return "", err
	}
	for _, t := range existing {
		if t.Relation == relation && subjectsEqual(t.Subject, subject) {
			return t.URI, nil
		}
	}

	value := map[string]any{
		"subject":   subject.toInterface(),
		"relation":  string(relation),
		"object":    objectToInterface(object),
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}

	// Write the org-owned record and the FGA tuple in one transaction: the FGA
	// write runs last inside the closure, so if it fails the record is rolled
	// back, keeping the two stores in sync.
	var uri habitat_syntax.SpaceRecordURI
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var perr error
		uri, _, perr = s.spaces.WithTx(tx).PutRecord(
			ctx,
			object,
			object.SpaceOwner(),
			tupleCollection,
			"",
			value,
		)
		if perr != nil {
			return perr
		}
		return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey(fgastore.SpaceObjectKey(object), fgaRelation, fgaUser),
				},
				OnDuplicate: "ignore",
			},
		})
	})
	if err != nil {
		return "", err
	}
	return uri, nil
}

// DeleteTuple removes the tuple at uri from both FGA and the governing space.
func (s *Store) DeleteTuple(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	space := uri.SpaceURI()
	collection := uri.Collection()
	repo := uri.Repo()
	rkey := uri.Rkey()
	if space == "" || collection == "" || repo == "" || rkey == "" {
		return fmt.Errorf("%w: malformed tuple uri", ErrTupleNotFound)
	}

	rec, err := s.spaces.GetRecord(ctx, space, repo, collection, rkey)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrTupleNotFound
	} else if err != nil {
		return err
	}

	subject, relation, object, err := parseTupleValue(rec.Value)
	if err != nil {
		return err
	}
	fgaRelation, err := roleToFGARelation(relation)
	if err != nil {
		return err
	}
	fgaUser, err := subject.fgaUserString()
	if err != nil {
		return err
	}

	// Delete the record and the FGA tuple in one transaction: the record delete
	// runs first, so if the FGA delete fails the record delete is rolled back.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.spaces.WithTx(tx).
			DeleteRecord(ctx, space, collection, rkey.String()); err != nil {
			return err
		}
		return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					tuple.TupleKeyToTupleKeyWithoutCondition(
						tuple.NewTupleKey(fgastore.SpaceObjectKey(object), fgaRelation, fgaUser),
					),
				},
				OnMissing: "ignore",
			},
		})
	})
}

// ListTuples returns the tuples governing a space, applying optional filters.
func (s *Store) ListTuples(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	filter ListTuplesFilter,
) ([]TupleView, error) {
	all, err := s.listTuples(ctx, space)
	if err != nil {
		return nil, err
	}

	filtered := make([]TupleView, 0, len(all))
	for _, view := range all {
		if filter.Object != nil && view.Object != *filter.Object {
			continue
		}
		if filter.Relation != nil && view.Relation != *filter.Relation {
			continue
		}
		if filter.SubjectKind != "" && view.Subject.Kind() != filter.SubjectKind {
			continue
		}
		if filter.SubjectDID != nil {
			if view.Subject.Kind() != SubjectKindUser {
				continue
			}
			got, err := view.Subject.fgaUserString()
			if err != nil || got != fgastore.MemberUserString(*filter.SubjectDID) {
				continue
			}
		}
		filtered = append(filtered, view)
	}
	return filtered, nil
}

// listTuples returns every tuple record in the space, decoded.
func (s *Store) listTuples(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]TupleView, error) {
	collection := tupleCollection
	records, err := s.spaces.ListRecords(ctx, space, space.SpaceOwner(), &collection)
	if err != nil {
		return nil, err
	}
	views := make([]TupleView, 0, len(records))
	for _, rec := range records {
		subject, relation, object, err := parseTupleValue(rec.Value)
		if err != nil {
			return nil, err
		}
		views = append(views, TupleView{
			URI: habitat_syntax.ConstructSpaceRecordURI(
				space,
				rec.Owner,
				rec.Collection,
				rec.Rkey,
			),
			Subject:  subject,
			Relation: relation,
			Object:   object,
		})
	}
	return views, nil
}

// Check reports whether did holds role on space, resolving usersets and the
// built-in role implications.
func (s *Store) Check(
	ctx context.Context,
	org syntax.DID,
	did syntax.DID,
	role Role,
	space habitat_syntax.SpaceURI,
) (bool, error) {
	fgaRelation, err := roleToFGARelation(role)
	if err != nil {
		return false, err
	}
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelation,
		fgastore.SpaceObjectKey(space),
		ownerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(org),
	)
}

// ListSubjects returns the user DIDs that hold role on space, expanding
// usersets and role implications.
func (s *Store) ListSubjects(
	ctx context.Context,
	org syntax.DID,
	space habitat_syntax.SpaceURI,
	role Role,
) ([]syntax.DID, error) {
	fgaRelation, err := roleToFGARelation(role)
	if err != nil {
		return nil, err
	}
	users, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(space),
		fgaRelation,
		ownerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(org),
	)
	if err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, 0, len(users))
	for _, u := range users {
		did, err := fgastore.MemberUserToDID(u)
		if err != nil {
			continue
		}
		dids = append(dids, did)
	}
	return dids, nil
}

// ListObjects returns the spaces on which did holds role, expanding usersets
// and role implications.
func (s *Store) ListObjects(
	ctx context.Context,
	org syntax.DID,
	did syntax.DID,
	role Role,
) ([]habitat_syntax.SpaceURI, error) {
	fgaRelation, err := roleToFGARelation(role)
	if err != nil {
		return nil, err
	}
	objects, err := s.fga.ListObjects(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelation,
		fgastore.TypeSpace,
		fgastore.OrgMemberContextualTuple(org),
	)
	if err != nil {
		return nil, err
	}
	spaceURIs := make([]habitat_syntax.SpaceURI, 0, len(objects))
	for _, key := range objects {
		uri, err := fgastore.ParseSpaceObjectKey(key)
		if err != nil {
			continue
		}
		spaceURIs = append(spaceURIs, uri)
	}
	return spaceURIs, nil
}

// parseTupleValue decodes the subject/relation/object from a stored tuple
// record value.
func parseTupleValue(value map[string]any) (Subject, Role, habitat_syntax.SpaceURI, error) {
	subject, err := ParseSubject(value["subject"])
	if err != nil {
		return nil, "", "", err
	}
	relationStr, ok := value["relation"].(string)
	if !ok {
		return nil, "", "", fmt.Errorf("%w: tuple missing relation", ErrInvalidTuple)
	}
	relation := Role(relationStr)
	if _, err := roleToFGARelation(relation); err != nil {
		return nil, "", "", err
	}
	object, err := parseObject(value["object"])
	if err != nil {
		return nil, "", "", err
	}
	return subject, relation, object, nil
}

// subjectsEqual reports whether two subjects denote the same FGA user.
func subjectsEqual(a, b Subject) bool {
	sa, errA := a.fgaUserString()
	sb, errB := b.fgaUserString()
	return errA == nil && errB == nil && sa == sb
}
