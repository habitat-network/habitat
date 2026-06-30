package fgastore

import (
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

const (
	TypeOrganization           = "organization"
	TypeUser                   = "user"
	TypeSpace                  = "space"
	RelationAdmin              = "admin"
	RelationMember             = "member"
	RelationSpaceOwner         = "owner"
	RelationSpaceReader        = "can_read"
	RelationSpaceWriter        = "can_write"
	RelationSpaceMemberManager = "can_manage_members"
)

// spaceDirectlyRelatedUserTypes returns the set of user types that may be
// granted a space relation directly. Besides individual users, each space
// relation accepts space usersets (e.g. "space:A#can_read") so that a space —
// including a group-space — can be used as a grantee on another space. This is
// what powers groups-as-spaces, nested groups, and cross-space role
// inheritance: the relationship store grants "all holders of role R on space A"
// a role on space B by writing a space userset tuple.
func spaceDirectlyRelatedUserTypes() []*openfgav1.RelationReference {
	refs := []*openfgav1.RelationReference{{Type: TypeUser}}
	for _, rel := range []string{
		RelationSpaceOwner,
		RelationSpaceReader,
		RelationSpaceWriter,
		RelationSpaceMemberManager,
	} {
		refs = append(refs, &openfgav1.RelationReference{
			Type:               TypeSpace,
			RelationOrWildcard: &openfgav1.RelationReference_Relation{Relation: rel},
		})
	}
	return refs
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
							DirectlyRelatedUserTypes: spaceDirectlyRelatedUserTypes(),
						},
						RelationSpaceReader: {
							DirectlyRelatedUserTypes: spaceDirectlyRelatedUserTypes(),
						},
						RelationSpaceWriter: {
							DirectlyRelatedUserTypes: spaceDirectlyRelatedUserTypes(),
						},
						RelationSpaceMemberManager: {
							DirectlyRelatedUserTypes: spaceDirectlyRelatedUserTypes(),
						},
					},
				},
			},
		},
	}
}
