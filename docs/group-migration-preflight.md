# Legacy group migration preflight

Use `pear run group-preflight` on a synthetic or separately approved, isolated snapshot of the current Pear database schema. Supply the member domain configured on the source, not the hostname of the copy. This command does not connect to a running Pear or initialize its services.

```sh
pear run group-preflight --member-domain example.com \
  --sqlite-snapshot /absolute/path/to/standalone-backup.db
```

Use a standalone SQLite backup made with SQLite's backup API or `VACUUM INTO`, not a copy of a live database file. Do not modify the backup while reading it. For PostgreSQL, set `PEAR_PREFLIGHT_POSTGRES_DSN` through your normal secret handling to a restored, isolated copy with a read-only account, then run:

```sh
pear run group-preflight --member-domain example.com --postgres-snapshot
```

The command opens SQLite in read-only immutable mode and uses a read-only, repeatable-read transaction for PostgreSQL. It does not apply migrations. Do not point it at a live instance.

## Reading the report

- `candidate_groups` counts DIDs with legacy evidence (including hosted group-handle candidates) that do not have a known completion marker. Email-domain mappings, pending invitations, legacy spaces or records, and local legacy sharing subjects can reveal otherwise incomplete or orphaned candidates. Absence of a mapping does not prove which route provisioned the group.
- The source counters overlap. `active_legacy_group_records` includes legacy collection rows and app-access rows in the legacy members space, excluding tombstones. `spaces_with_legacy_access` counts distinct source space triples with an active access record. `local_legacy_subject_share_records` counts active relationship records with a recognized local legacy roster subject, not effective grants. `invalid_candidate_rows` counts evidence that could not be attributed, such as a missing identity ID or unreadable share record.
- Only an explicit host-local completion marker reports `present`. Older schemas report `schema_unavailable`; a readable marker table with no row for a DID reports `absent`. Canonical space existence does not substitute for a marker. Marked DIDs are listed separately from the candidate count, with legacy evidence still visible, but without legacy conversion blockers.
- Admin role counts reflect structurally valid group-authored legacy membership records. They do not verify who can act as an admin today. Domain mappings do not imply membership; a legacy acceptance record is not proof of personal confirmation. Non-default, empty, or unreadable invite roles require review of the original intent. Never substitute broader roles.

The JSON identifies group DIDs, counts, and categorical findings. It omits handles, member and invitee DIDs, domain mappings' names, role names, record contents, keys, and connection URLs. Keep the report and snapshot in approved operator storage.

Resolve incomplete evidence and compare actual legacy and canonical access decisions before cutover. A successful run, a group without blockers, or a completion marker is not proof of readiness, effective access parity, or authority to migrate. The command does not inspect live FGA state, downstream search and sharing projections, blob attribution, or personal confirmation provenance.
