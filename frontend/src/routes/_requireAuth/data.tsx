import {
  createFileRoute,
  useNavigate,
  useRouter,
} from "@tanstack/react-router";
import { useMemo } from "react";

interface SearchParams {
  lexicon?: string;
  isPrivate?: boolean;
  repoDid?: string;
  filter?: string;
}

interface FilterCriteria {
  [key: string]: string;
}

// Parse filter text into key-value pairs
const parseFilters = (text: string): FilterCriteria => {
  const filters: FilterCriteria = {};
  const parts = text.trim().split(/\s+/);

  for (const part of parts) {
    if (part.includes(":")) {
      const [key, ...valueParts] = part.split(":");
      const value = valueParts.join(":"); // Handle values that might contain colons
      if (key && value) {
        filters[key] = value;
      }
    }
  }

  return filters;
};

export const Route = createFileRoute("/_requireAuth/data")({
  validateSearch(search: Record<string, unknown>): SearchParams {
    return {
      lexicon: (search.lexicon as string) || undefined,
      isPrivate: search.isPrivate === true || search.isPrivate === "true",
      repoDid: (search.repoDid as string) || undefined,
      filter: (search.filter as string) || undefined,
    };
  },
  loaderDeps: ({ search }) => ({
    lexicon: search.lexicon,
    isPrivate: search.isPrivate,
    repoDid: search.repoDid,
  }),
  async loader({ deps: { lexicon, isPrivate, repoDid }, context }) {
    if (!lexicon) {
      return { records: [], error: null };
    }

    try {
      // Use the repo DID if provided, otherwise undefined (uses default)
      const repo = repoDid?.trim() || undefined;
      if (isPrivate) {
        const data = await context.authManager
          .client()
          .listPrivateRecords(lexicon, undefined, undefined, repo ? [repo] : undefined)
        return { records: data.records, error: null };
      } else {
        const data = await context.authManager
          .client()
          .listRecords(lexicon, undefined, undefined, repo);
        return { records: data.records, error: null };
      }
    } catch (err) {
      return {
        records: [],
        error: err instanceof Error ? err.message : "An unknown error occurred",
      };
    }
  },
  component: DataDebugger,
});

function DataDebugger() {
  const { lexicon, isPrivate, repoDid, filter } = Route.useSearch();
  const { records, error } = Route.useLoaderData();
  const navigate = useNavigate({ from: Route.fullPath });
  const router = useRouter();

  const parsedFilters = useMemo(() => parseFilters(filter || ""), [filter]);

  const filteredRecords = useMemo(() => {
    if (!records || records.length === 0) return [];

    if (Object.keys(parsedFilters).length === 0) {
      return records;
    }

    return records.filter((record) => {
      for (const [key, value] of Object.entries(parsedFilters)) {
        if (key === "rkey") {
          const rkey = record.uri?.split("/").pop();
          if (!rkey || !rkey.includes(value)) {
            return false;
          }
        } else {
          const recordValue = record.value as Record<string, unknown>;
          const fieldValue = recordValue[key];

          if (fieldValue === undefined) {
            return false;
          }

          if (!String(fieldValue).includes(value)) {
            return false;
          }
        }
      }

      return true;
    });
  }, [records, parsedFilters]);

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const isPrivateEl = form.elements.namedItem("isPrivate") as HTMLInputElement;
    navigate({
      search: () => ({
        lexicon: (data.get("lexicon") as string) || undefined,
        repoDid: (data.get("repoDid") as string) || undefined,
        filter: (data.get("filter") as string) || undefined,
        isPrivate: isPrivateEl?.checked || undefined,
      }),
      replace: true,
    });
  };

  return (
    <div style={{ padding: "1.5rem" }}>
      <h1
        style={{ fontSize: "1.5rem", fontWeight: "bold", marginBottom: "1rem" }}
      >
        Data Debugger
      </h1>

      {/* Top bar with controls */}
      <form
        key={`${lexicon || ""}-${repoDid || ""}-${filter || ""}-${isPrivate}`}
        onSubmit={handleSubmit}
        style={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: "1rem",
          padding: "0.75rem 1rem",
          borderRadius: "6px",
          marginBottom: "1.5rem",
          border: "1px solid ButtonBorder",
          colorScheme: "light dark",
        }}
      >
        {/* Lexicon Input */}
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <label
            htmlFor="lexicon"
            style={{ fontWeight: 500, color: "CanvasText" }}
          >
            Lexicon:
          </label>
          <input
            id="lexicon"
            name="lexicon"
            type="text"
            defaultValue={lexicon || ""}
            placeholder="e.g., app.bsky.feed.post"
            style={{
              border: "1px solid ButtonBorder",
              borderRadius: "4px",
              backgroundColor: "Field",
              color: "FieldText",
              fontSize: "0.8rem",
              width: "280px",
            }}
          />
        </div>

        {/* Repo DID Input */}
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <label
            htmlFor="repoDid"
            style={{ fontWeight: 500, color: "CanvasText" }}
          >
            DID:
          </label>
          <input
            id="repoDid"
            name="repoDid"
            type="text"
            defaultValue={repoDid || ""}
            placeholder="did:plc:..."
            style={{
              border: "1px solid ButtonBorder",
              borderRadius: "4px",
              fontFamily: "monospace",
              backgroundColor: "Field",
              color: "FieldText",
              fontSize: "0.8rem",
              width: "280px",
            }}
          />
        </div>

        {/* Filter Input */}
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <label
            htmlFor="filter"
            style={{ fontWeight: 500, color: "CanvasText" }}
          >
            Filter:
          </label>
          <input
            id="filter"
            name="filter"
            type="text"
            defaultValue={filter || ""}
            placeholder="key:value"
            style={{
              padding: "0.375rem 0.5rem",
              border: "1px solid ButtonBorder",
              borderRadius: "4px",
              backgroundColor: "Field",
              color: "FieldText",
              fontSize: "0.8rem",
            }}
          />
        </div>

        {/* Privacy Checkbox */}
        <label
          style={{
            display: "flex",
            alignItems: "center",
            gap: "0.375rem",
            cursor: "pointer",
            color: "CanvasText",
          }}
        >
          <input
            type="checkbox"
            name="isPrivate"
            defaultChecked={isPrivate || false}
          />
          <span style={{ fontWeight: 500 }}>Private Data</span>
        </label>

        {/* Refresh Button */}
        <button
          type="submit"
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            padding: "0.25rem",
            fontSize: "0.8rem",
            color: "GrayText",
            textDecoration: "underline",
          }}
        >
          Refresh
        </button>

        {/* Show active filters */}
        {Object.keys(parsedFilters).length > 0 && (
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: "0.5rem",
              marginLeft: "auto",
            }}
          >
            <span style={{ color: "GrayText", fontSize: "0.875rem" }}>
              Active:
            </span>
            {Object.entries(parsedFilters).map(([key, value]) => (
              <span
                key={key}
                style={{
                  backgroundColor: "Highlight",
                  color: "HighlightText",
                  padding: "0.125rem 0.5rem",
                  borderRadius: "4px",
                  fontSize: "0.875rem",
                }}
              >
                <span style={{ fontWeight: 500 }}>{key}:</span>
                <span>{value}</span>
              </span>
            ))}
          </div>
        )}
      </form>

      {/* Data Display */}
      <div>
        {!lexicon && (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              padding: "4rem 2rem",
              color: "GrayText",
              border: "2px dashed GrayText",
              borderRadius: "8px",
              opacity: 0.7,
            }}
          >
            <div style={{ fontSize: "3rem", marginBottom: "1rem" }}>🔍</div>
            <h3 style={{ margin: 0, fontSize: "1.25rem", fontWeight: 500 }}>
              Enter a Lexicon
            </h3>
            <p style={{ margin: "0.5rem 0 0", fontSize: "0.875rem" }}>
              Type a lexicon (e.g.,{" "}
              <code
                style={{
                  backgroundColor: "ButtonFace",
                  padding: "0.125rem 0.375rem",
                  borderRadius: "3px",
                }}
              >
                app.bsky.feed.post
              </code>
              ) to view records
            </p>
          </div>
        )}

        {error && lexicon && (
          <div>
            <div>
              <div>⚠️</div>
              <div>
                <h3>Error loading records</h3>
                <p>{error}</p>
                <button onClick={() => router.invalidate()}>Try again</button>
              </div>
            </div>
          </div>
        )}

        {!error && lexicon && (
          <>
            <div
              style={{
                marginBottom: "0.75rem",
                color: "GrayText",
                fontSize: "0.875rem",
              }}
            >
              {filteredRecords.length} of {records?.length || 0} record(s)
              {Object.keys(parsedFilters).length > 0 && " (filtered)"}
            </div>

            {filteredRecords && filteredRecords.length > 0 ? (
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: "1rem",
                }}
              >
                {filteredRecords.map((record) => (
                  <div
                    key={record.uri}
                    style={{
                      border: "1px solid rgba(128, 128, 128, 0.25)",
                      borderRadius: "8px",
                      padding: "1rem",
                      backgroundColor: "Canvas",
                      colorScheme: "light dark",
                    }}
                  >
                    <div
                      style={{
                        fontSize: "0.75rem",
                        fontFamily: "monospace",
                        color: "GrayText",
                        marginBottom: "0.75rem",
                        wordBreak: "break-all",
                      }}
                    >
                      {record.uri}
                    </div>
                    <pre
                      style={{
                        margin: 0,
                        padding: "0.75rem",
                        backgroundColor: "Field",
                        border: "1px solid ButtonBorder",
                        borderRadius: "4px",
                        fontFamily: "monospace",
                        fontSize: "0.8rem",
                        overflow: "auto",
                        whiteSpace: "pre-wrap",
                        wordBreak: "break-word",
                        color: "FieldText",
                      }}
                    >
                      {JSON.stringify(record.value, null, 2)}
                    </pre>
                  </div>
                ))}
              </div>
            ) : (
              <div>
                <div>📭</div>
                <h3>
                  {Object.keys(parsedFilters).length > 0
                    ? "No matching records"
                    : "No records found"}
                </h3>
                <p>
                  {Object.keys(parsedFilters).length > 0
                    ? "Try adjusting your filters"
                    : `No records found for collection "${lexicon}"`}
                </p>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
