import type { SearchResultView } from "@/server/fns/mcp";

const DOCS_COLLECTION = "network.habitat.docs.markdown";

export type ResultLink =
  | { kind: "doc"; docId: string }
  | { kind: "external"; href: string };

// resultLink says where a search hit's "Open" button should go. Habitat
// docs open in the app's own doc route (a record's url is a raw at:// URI,
// which browsers block). Other results open only if their url is http(s).
export function resultLink(r: SearchResultView): ResultLink | null {
  if (r.collection === DOCS_COLLECTION && r.space) {
    return { kind: "doc", docId: r.space };
  }
  if (r.url) {
    try {
      const u = new URL(r.url);
      if (u.protocol === "http:" || u.protocol === "https:") {
        return { kind: "external", href: r.url };
      }
    } catch {
      // not a URL
    }
  }
  return null;
}
