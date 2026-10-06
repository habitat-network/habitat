import { useEffect } from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { AuthManager } from "internal";
import { myOrgsQueryOptions } from "@/queries/opensocial";

interface SelectedOrgState {
  selectedOrg?: string;
  select: (did: string) => void;
}

// useSelectedOrgStore holds the org the user last picked, persisted to
// localStorage so it survives reloads.
const useSelectedOrgStore = create<SelectedOrgState>()(
  persist(
    (set) => ({
      selectedOrg: undefined,
      select: (did) => set({ selectedOrg: did }),
    }),
    {
      name: "habitat.selectedOrg",
      partialize: (state) => ({ selectedOrg: state.selectedOrg }),
    },
  ),
);

// useSelectedOrg resolves which org the app is currently scoped to. An org in
// the URL (/orgs/$org/...) wins, then the last org the user picked, then the
// first org they belong to. Visiting an org's URL makes it the picked one.
export function useSelectedOrg(authManager: AuthManager) {
  const { data: orgs = [] } = useQuery(myOrgsQueryOptions(authManager));
  const { org: urlOrg } = useParams({ strict: false }) as { org?: string };
  const selectedOrg = useSelectedOrgStore((s) => s.selectedOrg);
  const select = useSelectedOrgStore((s) => s.select);

  useEffect(() => {
    if (urlOrg) select(urlOrg);
  }, [urlOrg, select]);

  const known = (did?: string) => orgs.find((o) => o.did === did)?.did;
  const org = known(urlOrg) ?? known(selectedOrg) ?? orgs[0]?.did;
  return { org, orgs, select };
}
