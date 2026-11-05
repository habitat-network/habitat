import { create } from "zustand";
import { persist } from "zustand/middleware";

// Everything in Brain happens in the context of one org: its chats, its
// search, its LLM models and its MCP tools. Only the DID is kept — never the
// name — so the org list (fetched live) stays the source of truth, and a
// selection the member no longer belongs to is treated as no selection.
interface OrgState {
  selectedOrg: string | null;
  selectOrg: (did: string | null) => void;
}

export const useOrgStore = create<OrgState>()(
  persist(
    (set) => ({
      selectedOrg: null,
      selectOrg: (did) => set({ selectedOrg: did }),
    }),
    { name: "brain-selected-org" },
  ),
);
