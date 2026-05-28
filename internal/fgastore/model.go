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
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
						RelationSpaceReader: {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
						RelationSpaceWriter: {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
						RelationSpaceMemberManager: {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: TypeUser},
							},
						},
					},
				},
			},
		},
	}
}
