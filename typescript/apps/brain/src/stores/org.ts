import { create } from "zustand";
import { persist } from "zustand/middleware";

// Everything in Brain happens in the context of one org: its chats, its
// search, its LLM models and its MCP tools. Only the DID is kept — never the
// name — so the org list (fetched live) stays the source of truth, and a
// selection the member no longer belongs to is treated as no selection.
//
// The model is the member's pick from the org's gateway models. It is only a
// preference: the chat route falls back to the org's first model when the
// pick isn't one the org offers.
interface OrgState {
  selectedOrg: string | null;
  selectOrg: (did: string | null) => void;
  selectedModel: string | null;
  selectModel: (model: string | null) => void;
}

export const useOrgStore = create<OrgState>()(
  persist(
    (set) => ({
      selectedOrg: null,
      selectOrg: (did) => set({ selectedOrg: did }),
      selectedModel: null,
      selectModel: (model) => set({ selectedModel: model }),
    }),
    { name: "brain-selected-org" },
  ),
);
