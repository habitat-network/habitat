package fgastore

import (
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

const (
	TypeOrganization           = "organization"
	TypeUser                   = "user"
	TypeSpace                  = "space"
	TypeGroup                  = "group"
	RelationAdmin              = "admin"
	RelationMember             = "member"
	RelationSpaceOwner         = "owner"
	RelationSpaceReader        = "can_read"
	RelationSpaceWriter        = "can_write"
	RelationSpaceMemberManager = "can_manage_members"
	RelationGroupMember        = "member"
)

// usersetReference builds a RelationReference for the userset type#relation,
// used to allow indirect subjects (e.g. a group's members, or a space's
// writers) to be directly related to a relation.
func usersetReference(typ, relation string) *openfgav1.RelationReference {
	return &openfgav1.RelationReference{
		Type:               typ,
		RelationOrWildcard: &openfgav1.RelationReference_Relation{Relation: relation},
	}
}

// subjectTypes returns the set of user types that may be directly related to a
// space role or a group membership: concrete users, plus the usersets that
// represent groups, other spaces' roles (for cross-space inheritance), and
// whole-org roles.
func subjectTypes() []*openfgav1.RelationReference {
	return []*openfgav1.RelationReference{
		{Type: TypeUser},
		usersetReference(TypeGroup, RelationGroupMember),
		usersetReference(TypeSpace, RelationSpaceOwner),
		usersetReference(TypeSpace, RelationSpaceMemberManager),
		usersetReference(TypeSpace, RelationSpaceWriter),
		usersetReference(TypeSpace, RelationSpaceReader),
		usersetReference(TypeOrganization, RelationAdmin),
		usersetReference(TypeOrganization, RelationMember),
	}
}

func authModel() *openfgav1.AuthorizationModel {
	return &openfgav1.AuthorizationModel{
		SchemaVersion: "1.1",
		TypeDefinitions: []*openfgav1.TypeDefinition{
			{Type: TypeUser},
			{
				Type: TypeOrganization,
				Relations: map[string]*openfgav1.Userset{
					RelationAdmin: {Userset: &openfgav1.Userset_This{}},
					RelationMember: {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: RelationAdmin,
										},
									},
								},
							}},
						},
					},
				},
				Metadata: &openfgav1.Metadata{
					Relations: map[string]*openfgav1.RelationMetadata{
						RelationAdmin: {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
						RelationMember: {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
					},
				},
			},
			{
				Type: TypeSpace,
				Relations: map[string]*openfgav1.Userset{
					RelationSpaceOwner: {Userset: &openfgav1.Userset_This{}},
					RelationSpaceReader: {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: RelationSpaceWriter,
										},
									},
								},
							}},
						},
					},
					RelationSpaceWriter: {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: RelationSpaceOwner,
										},
									},
								},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: RelationSpaceMemberManager,
										},
									},
								},
							}},
						},
					},
					RelationSpaceMemberManager: {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: RelationSpaceOwner,
										},
									},
								},
							}},
						},
					},
				},
				Metadata: &openfgav1.Metadata{
					Relations: map[string]*openfgav1.RelationMetadata{
						RelationSpaceOwner: {
							DirectlyRelatedUserTypes: subjectTypes(),
						},
						RelationSpaceReader: {
							DirectlyRelatedUserTypes: subjectTypes(),
						},
						RelationSpaceWriter: {
							DirectlyRelatedUserTypes: subjectTypes(),
						},
						RelationSpaceMemberManager: {
							DirectlyRelatedUserTypes: subjectTypes(),
						},
					},
				},
			},
			{
				Type: TypeGroup,
				Relations: map[string]*openfgav1.Userset{
					RelationGroupMember: {Userset: &openfgav1.Userset_This{}},
				},
				Metadata: &openfgav1.Metadata{
					Relations: map[string]*openfgav1.RelationMetadata{
						RelationGroupMember: {
							DirectlyRelatedUserTypes: subjectTypes(),
						},
					},
				},
			},
		},
	}
}
