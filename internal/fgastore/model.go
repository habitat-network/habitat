package fgastore

import (
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

func authModel() *openfgav1.AuthorizationModel {
	return &openfgav1.AuthorizationModel{
		SchemaVersion: "1.1",
		TypeDefinitions: []*openfgav1.TypeDefinition{
			{Type: "user"},
			{
				Type: "organization",
				Relations: map[string]*openfgav1.Userset{
					"admin": {Userset: &openfgav1.Userset_This{}},
					"member": {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: "admin",
										},
									},
								},
							}},
						},
					},
				},
				Metadata: &openfgav1.Metadata{
					Relations: map[string]*openfgav1.RelationMetadata{
						"admin": {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: "user"},
							},
						},
						"member": {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: "user"},
							},
						},
					},
				},
			},
			{
				Type: "space",
				Relations: map[string]*openfgav1.Userset{
					"owner": {Userset: &openfgav1.Userset_This{}},
					"can_read": {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: "can_write",
										},
									},
								},
							}},
						},
					},
					"can_write": {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: "owner",
										},
									},
								},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: "can_manage_members",
										},
									},
								},
							}},
						},
					},
					"can_manage_members": {
						Userset: &openfgav1.Userset_Union{
							Union: &openfgav1.Usersets{Child: []*openfgav1.Userset{
								{Userset: &openfgav1.Userset_This{}},
								{
									Userset: &openfgav1.Userset_ComputedUserset{
										ComputedUserset: &openfgav1.ObjectRelation{
											Relation: "owner",
										},
									},
								},
							}},
						},
					},
				},
				Metadata: &openfgav1.Metadata{
					Relations: map[string]*openfgav1.RelationMetadata{
						"owner": {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: "user"},
								{
									Type: "organization",
									RelationOrWildcard: &openfgav1.RelationReference_Relation{
										Relation: "admin",
									},
								},
							},
						},
						"can_read": {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: "user"},
							},
						},
						"can_write": {
							DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
								{Type: "user"},
							},
						},
						"can_manage_members": {
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
