import type { DerivedConfig } from "./config";
import type { PearClient } from "./pearClient";

// How long a membership snapshot stays fresh. On a lookup miss within this
// window the miss is trusted (the user isn't in any org); after it, membership
// is re-fetched so newly added members are picked up without a restart.
const REFRESH_INTERVAL_MS = 60_000;

// OrgDirectory maps a user DID to the org they belong to. The orgs are the ones
// sap manages (GET /org/list); each org's membership is read on demand via
// relationship.listSubjects on the org's self space
// (ats://<org>/network.habitat.organization/self), where every member holds the
// reader role. The mapping is cached briefly so per-request lookups stay cheap.
export class OrgDirectory {
  private config: DerivedConfig;
  private pear: PearClient;
  private userToOrg = new Map<string, string>();
  private fetchedAt = 0;
  private refreshing: Promise<void> | undefined;

  constructor(config: DerivedConfig, pear: PearClient) {
    this.config = config;
    this.pear = pear;
  }

  // orgForUser returns the org DID the user belongs to, or undefined if they
  // are not a member of any org sap manages. Membership is refreshed when the
  // cached snapshot is stale or on a miss (so a just-added member isn't locked
  // out for the refresh interval).
  async orgForUser(did: string): Promise<string | undefined> {
    if (this.stale() || !this.userToOrg.has(did)) {
      await this.refresh();
    }
    return this.userToOrg.get(did);
  }

  private stale(): boolean {
    return Date.now() - this.fetchedAt > REFRESH_INTERVAL_MS;
  }

  // refresh re-fetches org memberships, deduping concurrent callers onto one
  // in-flight fetch.
  private refresh(): Promise<void> {
    if (!this.refreshing) {
      this.refreshing = this.fetchMemberships().finally(() => {
        this.refreshing = undefined;
      });
    }
    return this.refreshing;
  }

  private async fetchMemberships(): Promise<void> {
    const res = await fetch(`${this.config.sapUrl}/org/list`);
    if (!res.ok) {
      throw new Error(`sap /org/list failed (${res.status})`);
    }
    const { orgs } = (await res.json()) as { orgs: string[] };

    const next = new Map<string, string>();
    for (const org of orgs) {
      let members: string[];
      try {
        members = await this.pear.listOrgMembers(org);
      } catch (err) {
        // Keep serving the previous snapshot for orgs that fail (e.g. sap's
        // session expired); other orgs still refresh.
        console.error("[org-directory] list members failed", org, err);
        for (const [user, userOrg] of this.userToOrg) {
          if (userOrg === org) {
            next.set(user, org);
          }
        }
        continue;
      }
      for (const member of members) {
        next.set(member, org);
      }
    }
    this.userToOrg = next;
    this.fetchedAt = Date.now();
  }
}
