import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import {
  Badge,
  Button,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Spinner,
} from "internal/components/ui";
import {
  CircleAlert,
  ExternalLink,
  Search as SearchIcon,
  X,
} from "lucide-react";
import { useState } from "react";
import { EmptyState, PageHeader } from "@/components/shell/PageHeader";
import { cn } from "@/lib/cn";
import {
  searchGroup,
  type SearchBackend,
  type SearchResultView,
} from "@/server/fns/mcp";

// Search runs one query across the group's Habitat records and every MCP
// server the member has connected (pear's network.habitat.mcp.search).
// The left column filters by source, the middle lists results, and the
// right previews the selected one.

export const Route = createFileRoute("/_member/g/$org/search")({
  validateSearch: z.object({ q: z.string().optional() }),
  component: SearchPage,
});

function SearchPage() {
  const { org } = Route.useParams();
  const { q = "" } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<SearchResultView | null>(null);

  const search = useInfiniteQuery({
    queryKey: ["search", org, q],
    enabled: q.trim() !== "",
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      searchGroup({
        data: pageParam ? { org, cursor: pageParam } : { org, query: q },
      }),
    getNextPageParam: (last) => last.cursor ?? undefined,
  });
  const pages = search.data?.pages ?? [];
  const results = pages.flatMap((p) => p.results);
  // The latest page has every backend's current status.
  const backends = pages.at(-1)?.backends ?? [];
  const shown = results.filter((r) => !hidden.has(r.backend));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Search"
        description="Your group's records and your connected tools"
      />
      <SearchForm
        key={q}
        q={q}
        busy={search.isFetching}
        onSearch={(next) => {
          setSelected(null);
          void navigate({ search: { q: next || undefined } });
        }}
      />

      <div className="flex min-h-0 flex-1">
        <aside className="hidden w-60 shrink-0 flex-col gap-1 border-r p-3 md:flex">
          <p className="px-2 pb-1 text-xs font-medium text-muted-foreground">
            Sources
          </p>
          {backends.length === 0 && (
            <p className="px-2 text-sm text-muted-foreground">
              Search to see sources.
            </p>
          )}
          {backends.map((b) => (
            <BackendFilter
              key={b.id}
              backend={b}
              on={!hidden.has(b.id)}
              onToggle={() =>
                setHidden((h) => {
                  const next = new Set(h);
                  if (next.has(b.id)) next.delete(b.id);
                  else next.add(b.id);
                  return next;
                })
              }
            />
          ))}
        </aside>

        <div className="flex min-w-0 flex-1 flex-col gap-2 overflow-y-auto p-4">
          {!q ? (
            <EmptyState icon={<SearchIcon />} title="Search your group">
              Docs, files and records in the group, plus results from the MCP
              servers you've connected.
            </EmptyState>
          ) : search.isError ? (
            <EmptyState icon={<CircleAlert />} title="Search failed">
              {search.error.message}
            </EmptyState>
          ) : search.isPending ? null : shown.length === 0 ? (
            <EmptyState icon={<SearchIcon />} title={`No results for “${q}”`} />
          ) : (
            shown.map((r) => (
              <button
                key={`${r.backend}:${r.id}`}
                type="button"
                onClick={() => setSelected(r)}
                className={cn(
                  "flex flex-col gap-1 rounded-xl border p-3 text-left hover:bg-muted/50",
                  selected?.id === r.id &&
                    selected.backend === r.backend &&
                    "border-primary bg-primary/5",
                )}
              >
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">
                    {r.title ?? r.id}
                  </span>
                  <Badge variant="secondary" className="ml-auto shrink-0">
                    {backendName(backends, r.backend)}
                  </Badge>
                </div>
                {r.snippet && (
                  <p className="line-clamp-2 text-sm text-muted-foreground">
                    {r.snippet}
                  </p>
                )}
              </button>
            ))
          )}
          {search.hasNextPage && (
            <Button
              variant="outline"
              className="self-center"
              disabled={search.isFetchingNextPage}
              onClick={() => search.fetchNextPage()}
            >
              Load more
            </Button>
          )}
        </div>

        {selected && (
          <aside className="hidden w-80 shrink-0 flex-col gap-4 overflow-y-auto border-l p-5 lg:flex">
            <div className="flex items-start justify-between gap-2">
              <h2 className="font-semibold break-words">
                {selected.title ?? selected.id}
              </h2>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Close preview"
                onClick={() => setSelected(null)}
              >
                <X />
              </Button>
            </div>
            <Badge variant="secondary">
              {backendName(backends, selected.backend)}
            </Badge>
            {selected.snippet && (
              <p className="text-sm whitespace-pre-wrap">{selected.snippet}</p>
            )}
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-muted-foreground">Kind</dt>
              <dd>{selected.kind}</dd>
              {selected.collection && (
                <>
                  <dt className="text-muted-foreground">Collection</dt>
                  <dd className="break-all">{selected.collection}</dd>
                </>
              )}
              {selected.space && (
                <>
                  <dt className="text-muted-foreground">Space</dt>
                  <dd className="break-all">{selected.space}</dd>
                </>
              )}
            </dl>
            {selected.url && (
              <Button
                variant="outline"
                render={
                  <a href={selected.url} target="_blank" rel="noreferrer" />
                }
              >
                <ExternalLink data-icon="inline-start" />
                Open
              </Button>
            )}
          </aside>
        )}
      </div>
    </div>
  );
}

// SearchForm is the query box. It's keyed by the query in the URL, so
// navigating to another search resets what's typed.
function SearchForm({
  q,
  busy,
  onSearch,
}: {
  q: string;
  busy: boolean;
  onSearch: (q: string) => void;
}) {
  const [draft, setDraft] = useState(q);
  return (
    <form
      className="border-b p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSearch(draft.trim());
      }}
    >
      <InputGroup className="mx-auto max-w-3xl">
        <InputGroupAddon>
          <SearchIcon />
        </InputGroupAddon>
        <InputGroupInput
          autoFocus
          placeholder="Search everything"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        {busy && (
          <InputGroupAddon align="inline-end">
            <Spinner />
          </InputGroupAddon>
        )}
      </InputGroup>
    </form>
  );
}

function backendName(backends: SearchBackend[], id: string) {
  return backends.find((b) => b.id === id)?.name ?? id;
}

function BackendFilter({
  backend,
  on,
  onToggle,
}: {
  backend: SearchBackend;
  on: boolean;
  onToggle: () => void;
}) {
  const failed = backend.status !== "ok" && backend.status !== "pending";
  return (
    <label
      className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-muted/60"
      title={backend.error}
    >
      <input
        type="checkbox"
        checked={on}
        onChange={onToggle}
        className="accent-primary"
      />
      <span className={cn("flex-1 truncate", !on && "text-muted-foreground")}>
        {backend.name}
      </span>
      {backend.status === "pending" ? (
        <Spinner className="size-3" />
      ) : failed ? (
        <span className="text-xs text-destructive">
          {backend.status === "not_connected"
            ? "not connected"
            : backend.status}
        </span>
      ) : (
        <span className="text-xs tabular-nums text-muted-foreground">
          {backend.count}
        </span>
      )}
    </label>
  );
}
