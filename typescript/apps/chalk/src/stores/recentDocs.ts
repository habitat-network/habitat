import { create } from "zustand";
import { persist } from "zustand/middleware";

// Only docIds are kept — never titles — so a rename elsewhere is reflected
// immediately wherever this list is rendered against the live docs query,
// with nothing here to go stale.
const MAX_RECENT_DOCS = 8;

interface RecentDocsState {
  recentDocIds: string[];
  addRecentDoc: (docId: string) => void;
}

export const useRecentDocsStore = create<RecentDocsState>()(
  persist(
    (set) => ({
      recentDocIds: [],
      addRecentDoc: (docId) =>
        set((state) => ({
          recentDocIds: [
            docId,
            ...state.recentDocIds.filter((id) => id !== docId),
          ].slice(0, MAX_RECENT_DOCS),
        })),
    }),
    { name: "chalk-recent-docs" },
  ),
);
