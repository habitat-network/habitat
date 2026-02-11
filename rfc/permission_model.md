# Permissions model

## Context

A usable permissions model requires both the granter and the grantee to have knowledge of the access granted. 
The granter needs to know who all has access to their data for visibility but also to support revocations. 
The grantee needs to know what it has access to in order to fetch it. 
In a decentralized network like Habitat where granter and grantee live on different nodes and don't share a centralized permission store, this permissions data needs to be synchronized between their nodes. 

## Proposal

The granter maintains an ACL (Access Control List) of which records it has granted permission to which users. 
The ACL is the definitive source of truth for the permissions of a record.
When a record's content or permissions are updated, the granter sends a message containing the permission info to the grantee.
The grantee verifies that authenticity of the message via service auth and stores the message in its inbox.
When the grantee's application requests a list of records, the grantee's node will reference it's inbox to resolve the request.
If the application wants to fetch the record, the grantee's node forwards the request to the granter's node.
The granter's node will authenticate the grantee's request using service auth and authorize the request by referencing its ACL.
By notifying the granter on record updates, the grantee can stay in sync by fetching the latest version of the record.

### Delegating permissions 

In order to support sharing of permissions across users, we allow a node to inherit permissions of a record from another record.
The parent record may be owned by a different user.
This enables a collection of records that all inherit from a parent record to form an "arena" of shared permissions that can be referenced by the parent record URI.
However, if the parent record updates permissions, the child records have had their permissions with no way of notifying all grantees (because the grantees are stored in the parent record's node ACL).
Furthermore, in order for a grantee to fetch the latest version of the child record, the granter cannot reference its ACL since the permissions are stored on another node.

In order to support this, the granter needs to notify the arena owner of record updates and the arena owner needs to propogate that notification to all grantees.
In this notification to grantees, the arena owner can provide a JWT that verifes the grantee has permission to the arena.
When the grantee fetches the records from the granter, it can use the JWT to prove to the granter that it has permission to the arena.
The granter can verify the JWT against the arena owner's public key. 
