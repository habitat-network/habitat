# 001 (WIP) Relationship-based Access Control for Spaces
Fine-grained permissioning is necessary to support the rich interactions of modern-day software. For example, a collaborative document editor or collaborative canvas may distinguish between an owner / editor / commentor / viewer, each role with varying degrees of what they can do within the app. We'd like to build these types of interactions on top of spaces in order to maintain data portability and interoperability within apps such as these. 

This can be done by introducing support for relationship-based access control to space hosts, in a way that allows users to have relations to spaces, and spaces to have relations to other spaces. In this spec, we are opinionated about what these relations can be, but not how they can be used or layered on top of each other. So, the relations that support the above example are not granting "owner", "editor", "commentor", and "viewer" relations to a given space, but rather *representing these roles* through the relationships between users <--> spaces and spaces <--> spaces. We will return to this example at the end of the spec.


## Terminology

## Supported Relationships

## Implementation

## XRPC API

All methods for the XRPC API are currently defined under the `network.habitat.relationship.*` namespace. As we finalize these further, we hope to formalize them under a community governed namespace. All methods are expected to be implemented by the space host. Relevant relation records must be written into the space they are governing in order to support interoperability and portability upon migration between space hosts.

Only certain roles can take certain actions; this is outlined below under the Auth column.

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
### Visibility
All readers of a space can see the relationships in a space. If there is a case where it is desired to hide the relations of a space, this can be achieved by creating another space which inherits the relations of the first space, and add the readers from which you want to hide the original relationships to that new space.

### ListSpaces
Eventually, we want to support calling `listSpaces` on behalf of another user, to see what spaces another user may have access to. Today, `listSpaces` only supports showing spaces the authed-user has a relation to, and we leave this as a future TODO for this spec.

### Syncing
Because all relations are written as records into the space, the [regular sync protocol of spaces](https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#sync) can be used. The synced records can then be used to recreate the permissions model within the app. However, because we support `checkUserRelation`, for simple use cases, syncers can also just sync the data records from spaces and look up on the space host what a DID's relation to the space is, and rely on the space host to resolve permissions.

## Open questions

**Interoperability**: Want to support "org A shares doc with group 1 from org B".

## Example
