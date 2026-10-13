import { create } from "zustand";
import { persist } from "zustand/middleware";

interface LastOrgState {
  lastOrg?: string;
  setLastOrg: (did: string) => void;
}

// useLastOrgStore remembers the most recently visited org, persisted to
// localStorage, so the index page can redirect to it and pages that aren't
// org-scoped (e.g. /spaces) can keep the header's org links. Org-scoped pages
// always read the current org from the route (/orgs/$org/...), never from here.
export const useLastOrgStore = create<LastOrgState>()(
  persist(
    (set) => ({
      lastOrg: undefined,
      setLastOrg: (did) => set({ lastOrg: did }),
    }),
    {
      name: "habitat.lastOrg",
      partialize: (state) => ({ lastOrg: state.lastOrg }),
    },
  ),
);

export const getLastOrg = () => useLastOrgStore.getState().lastOrg;
