# 001 Relationship-based Access Control for Spaces
Fine-grained permissioning is necessary

## Introduction
### Terminology

## Supported relationships
### Record keys

## XRPC API

All methods for the XRPC API are currently defined under the `network.habitat.relationship.*` namespace. As we finalize these further, we hope to formalize them under a community governed namespace. All methods are expected to be implemented by the space host. Relevant relation records must be written into the space they are governing in order to support interoperability and portability upon migration between space hosts.

Only cretain roles can take certain actions; this is outlined below under the Auth column.

| Method | Type | Auth | Description | 
|---|---|---|---|
| `createSpace` | procedure | OAuth | Create a space and grant the creator owner role on it. |
| `deleteSpace` | procedure | OAuth or space credential + owner or manager role | Create a space and grant the creator owner role on it. |
| `setUserRelation` | procedure | OAuth or space credential + owner or manager role | Create a relation between a DID and a space. |
| `setSpaceRelation` | procedure | OAuth or space credential + owner or manager role | Create a relation between the roles on one space and another space. |
| `deleteRelation` | procedure | OAuth or space credential + manager role | Delete a relation via the URI of its record. |
| `checkUserRelation` | query | OAuth or space credential + reader role | Check if DID has a given relation on a space. |
| `checkSpaceRelation` | query | OAuth or space credential + reader role | Check if space + role has a relation to a given space. |
| `listRelations` | query | OAuth or space credential + reader role | List all relations on a given space. |
| `resolveRelations` | query | OAuth or space credential + reader role | Resolve the DIDs that hold a given role on a space |
| `listSpaces` | query | OAuth | List all spaces the authed-user has a relation to. |


## Discussion
**Visibility of relations**: All readers of a space can see the relationships in a space. If there is a case where it is desired to hide the relations of a space, this can be achieved by creating another space which inherits the relations of the first space, and add the readers from which you want to hide the original relationships to that new space.


## Examples
