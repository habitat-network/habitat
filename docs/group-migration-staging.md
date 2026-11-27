# Prepare a private legacy group conversion plan

Start with the [read-only preflight](group-migration-preflight.md). Use a synthetic restored snapshot and the member domain configured on its source. Do not point this command at a live database.

Create a dedicated operator-controlled directory with mode `0700`. Use an absolute plan path inside it:

```sh
pear run group-stage --member-domain example.com \
  --sqlite-snapshot /absolute/path/to/standalone-backup.db \
  --group-did did:web:example-group.example.com \
  --plan-file /absolute/path/to/private-staging/attempt.json
```

For an isolated PostgreSQL restore, use `--postgres-snapshot` instead of `--sqlite-snapshot` and provide `PEAR_PREFLIGHT_POSTGRES_DSN` through approved secret handling. The snapshot transaction is read-only. The command does not initialize Pear or apply schema migrations.

## Review and retry

The mode `0600` file is planning evidence, not executable migration input. It preserves the existing DID and handle and lists the exact `(owner,type,skey)` addresses of the group's spaces. Candidate canonical locators use the same DID and a fixed invite-only requirement. No policy, canonical record, marker, moderation data, or identity is created.

The plan pins space and repo revisions, policies, relevant authoritative records and tombstones, Hive identity material, domain mappings, and pending invitations. It carries exact membership and invite role lists where readable. Exact permissions, app grants, invite recipients, and record payloads that are not embedded can be recovered from their pinned source locators or from the retained snapshot. Keep that snapshot. The plan may contain private addresses, record keys, member identifiers, and role names, so keep it out of source control, application logs and shared reports. It does not include raw key material, credentials, or the database URL.

Re-run the same command against the same path to verify the artifact. An identical plan leaves the existing file intact. A changed source or conflicting file is not overwritten. Choose a fresh attempt path when deliberately preparing a plan against a newer snapshot; retain or discard the earlier plan according to your recovery process. An occupied canonical namespace is a blocker, not permission to update or delete existing data. A group with a completion marker is refused.

Discard an attempt by removing only its own plan file from the private directory. There are no staging rows or changes to roll back in Pear. Do not remove any other attempt, existing canonical data, or the source snapshot.

## What still blocks activation

Do not guess role mappings, expand `canAssign`, infer admin authority from an email-domain mapping, or create personal confirmations from legacy records. A role-bearing invite requires verified equivalent reissue or compatibility; copying it into an unqualified canonical invite risks changing its grants. Keep legacy invite and email behavior until a verified cutover.

The pins describe an observed snapshot, not a live write fence or proof of effective access. Sharing retains original subject URI spellings and tombstones, but this command does not prove external FGA tuples, remote state, search ACLs, app authorization, blob attribution, or downstream projection parity. Activation must re-read the relevant inputs under a write fence and resolve every blocker before any authority change.
