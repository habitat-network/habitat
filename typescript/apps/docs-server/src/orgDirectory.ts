import type { DatabaseSync } from "node:sqlite";
import type { DerivedConfig } from "./config";
import type { PearClient } from "./pearClient";

// OrgDirectory maps a user DID to the org they belong to. The orgs are the ones
// sap manages (GET /org/list); each org's membership is read via
// relationship.listSubjects on the org's self space
// (ats://<org>/network.habitat.organization/self), where every member holds the
// reader role. The mapping is persisted to sqlite (shared with the doc stores)
// so it survives restarts. It is refreshed on demand rather than on an interval:
// sap forwards events on the network.habitat.organization space over the crawler
// channel, and each one triggers a refresh of that space's owning org (see
// Crawler), so newly added members are picked up without a restart. On startup
// every org sap manages is refreshed once.
export class OrgDirectory {
  private config: DerivedConfig;
  private pear: PearClient;
  private db: DatabaseSync;
  // Per-org in-flight refreshes, so concurrent callers for the same org share
  // one fetch.
  private refreshing = new Map<string, Promise<void>>();

  constructor(config: DerivedConfig, pear: PearClient, db: DatabaseSync) {
    this.config = config;
    this.pear = pear;
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS org_members (
        user_did TEXT PRIMARY KEY,
        org_did  TEXT NOT NULL
      );
    `);
  }

  // orgForUser returns the org DID the user belongs to, or undefined if they
  // are not a member of any org sap manages. This is a cheap read of the
  // persisted snapshot; the snapshot is kept current by refresh().
  orgForUser(did: string): string | undefined {
    const row = this.db
      .prepare(`SELECT org_did FROM org_members WHERE user_did = ?`)
      .get(did);
    return row ? String(row.org_did) : undefined;
  }

  // refreshAll refetches membership for every org sap manages. Used at startup
  // to seed the directory; per-org refreshes keep it current thereafter.
  async refreshAll(): Promise<void> {
    const res = await fetch(`${this.config.sapUrl}/org/list`);
    if (!res.ok) {
      throw new Error(`sap /org/list failed (${res.status})`);
    }
    const { orgs } = (await res.json()) as { orgs: string[] };
    // Refresh each org independently so one failing org doesn't block the rest;
    // failures are logged by refresh().
    await Promise.allSettled(orgs.map((org) => this.refresh(org)));
  }

  // refresh re-fetches a single org's membership from sap, deduping concurrent
  // callers for the same org onto one in-flight fetch. Called whenever sap
  // reports an update to that org's network.habitat.organization space.
  refresh(org: string): Promise<void> {
    let inflight = this.refreshing.get(org);
    if (!inflight) {
      inflight = this.fetchOrgMembers(org).finally(() => {
        this.refreshing.delete(org);
      });
      this.refreshing.set(org, inflight);
    }
    return inflight;
  }

  private async fetchOrgMembers(org: string): Promise<void> {
    let members: string[];
    try {
      members = await this.pear.listOrgMembers(org);
    } catch (err) {
      // Keep serving the persisted snapshot on failure (e.g. sap's session
      // expired); the next event or startup will retry.
      console.error("[org-directory] list members failed", org, err);
      return;
    }
    this.replaceOrgMembers(org, members);
  }

  // replaceOrgMembers swaps the persisted membership for a single org in one
  // transaction, so a member who left is dropped and a new member is added.
  private replaceOrgMembers(org: string, members: string[]): void {
    const del = this.db.prepare(`DELETE FROM org_members WHERE org_did = ?`);
    const ins = this.db.prepare(
      `INSERT INTO org_members (user_did, org_did) VALUES (?, ?)
       ON CONFLICT(user_did) DO UPDATE SET org_did = excluded.org_did`,
    );
    this.db.exec("BEGIN");
    try {
      del.run(org);
      for (const member of members) {
        ins.run(member, org);
      }
      this.db.exec("COMMIT");
    } catch (err) {
      this.db.exec("ROLLBACK");
      throw err;
    }
  }
}
