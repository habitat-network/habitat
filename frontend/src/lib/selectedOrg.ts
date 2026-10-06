import { useCallback, useEffect, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { myOrgsQueryOptions } from "@/queries/opensocial";

const STORAGE_KEY = "habitat.selectedOrg";

function readStored(): string | undefined {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

// useSelectedOrg resolves which org the app is currently scoped to. An org in
// the URL (/orgs/$org/...) wins, then the last org the user picked, then the
// first org they belong to. Visiting an org's URL makes it the picked one.
export function useSelectedOrg(authManager: AuthManager) {
  const { data: orgs = [] } = useQuery(myOrgsQueryOptions(authManager));
  const { org: urlOrg } = useParams({ strict: false }) as { org?: string };
  const [stored, setStored] = useState(readStored);

  const select = useCallback((did: string) => {
    setStored(did);
    try {
      localStorage.setItem(STORAGE_KEY, did);
    } catch {
      // Storage unavailable; the selection just won't survive a reload.
    }
  }, []);

  // Remember the org of the page being viewed for later visits elsewhere.
  useEffect(() => {
    if (!urlOrg) return;
    try {
      localStorage.setItem(STORAGE_KEY, urlOrg);
    } catch {
      // Storage unavailable; the selection just won't survive a reload.
    }
  }, [urlOrg]);

  const known = (did?: string) => orgs.find((o) => o.did === did)?.did;
  const org = known(urlOrg) ?? known(stored) ?? orgs[0]?.did;
  return { org, orgs, select };
}
