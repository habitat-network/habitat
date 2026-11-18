import { createFileRoute, Link } from "@tanstack/react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { SpaceRef } from "@atproto/syntax";
import type { DidString } from "@atproto/lex";
import { useEffect, useState, type FormEvent } from "react";
import { z } from "zod";
import {
  AlertTriangle,
  Database,
  ExternalLink,
  Plug,
  Search,
  Unplug,
} from "lucide-react";
import {
  Badge,
  Button,
  Card,
  CardContent,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Spinner,
  Tabs,
  TabsList,
  TabsTrigger,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "internal/components/ui";
import {
  ALL_SOURCES,
  fedSearchQueryOptions,
  type SearchBackend,
  type SearchPage,
  type SearchResult,
} from "@/queries/fedSearch";

export const Route = createFileRoute("/_requireAuth/orgs/$org/search")({
  validateSearch: z.object({
    q: z.string().default(""),
    source: z.string().default(ALL_SOURCES),
  }),
  component: SearchPage,
});

function SearchPage() {
  const { q, source } = Route.useSearch();
  const { authManager } = Route.useRouteContext();
  const navigate = Route.useNavigate();
  const [input, setInput] = useState(q);
  // Search one org at a time: the one in the URL.
  const { org } = Route.useParams();
  const orgDid = org as DidString;

  // The mixed stream always runs: its pages carry every source's status and
  // the cursor that opens each source's own stream.
  const mixed = useInfiniteQuery(
    fedSearchQueryOptions(authManager, orgDid, q, ALL_SOURCES, undefined),
  );
  const mixedPages = mixed.data?.pages ?? [];
  const sourceCursor = mixedPages[0]?.backends.find(
    (b) => b.id === source,
  )?.cursor;
  const single = useInfiniteQuery(
    fedSearchQueryOptions(authManager, orgDid, q, source, sourceCursor),
  );
  const active = source === ALL_SOURCES ? mixed : single;
  const pages = active.data?.pages ?? [];

  // Every page reports every source; the latest is the most current.
  const backends = latestBackends(mixedPages, single.data?.pages ?? []);
  const results = pages.flatMap((p) => p.results);
  const pending = backends.filter((b) => b.status === "pending");
  const failed = backends.filter(
    (b) => b.status !== "ok" && b.status !== "pending",
  );
  const activeBackend = backends.find((b) => b.id === source);
  const activePending =
    source === ALL_SOURCES
      ? pending.length > 0
      : activeBackend?.status === "pending";

  // While a source is still running, keep asking for the stream's next page:
  // the server holds each call until new results are ready (or 10s pass), so
  // late results appear on their own without a busy loop.
  const { hasNextPage, isFetchingNextPage, fetchNextPage, isError } = active;
  useEffect(() => {
    if (activePending && hasNextPage && !isFetchingNextPage && !isError) {
      void fetchNextPage();
    }
  }, [activePending, hasNextPage, isFetchingNextPage, isError, fetchNextPage]);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    navigate({ search: { q: input, source: ALL_SOURCES } });
  };
  const selectSource = (value: string) =>
    navigate({ search: { q, source: value }, replace: true });

  const ranking = mixedPages[0]?.ranking;
  const names = new Map(backends.map((b) => [b.id, b.name]));

  return (
    <div className="flex flex-col gap-6 py-6">
      <h1 className="text-2xl font-semibold">Search</h1>
      <form onSubmit={onSubmit}>
        <InputGroup>
          <InputGroupInput
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Search Habitat records and your connected apps"
            aria-label="Search"
          />
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
        </InputGroup>
      </form>

      {q.trim() !== "" && (
        <>
          {backends.length > 0 && (
            <SourceTabs
              backends={backends}
              total={results.length}
              source={source}
              onSelect={selectSource}
            />
          )}

          {pending.length > 0 && <PendingNotice backends={pending} />}
          {failed.length > 0 && <FailedSources backends={failed} org={org} />}

          {active.isError && (
            <Card>
              <CardContent className="flex items-center justify-between gap-4 py-4">
                <p className="text-destructive text-sm">
                  {active.error.message}
                </p>
                <Button variant="outline" onClick={() => active.refetch()}>
                  Search again
                </Button>
              </CardContent>
            </Card>
          )}

          {active.isPending && !active.isError && <ResultSkeletons />}

          {!active.isPending &&
            results.length === 0 &&
            !activePending &&
            !active.isError && (
              <p className="text-muted-foreground py-10 text-center text-sm">
                No results
                {activeBackend ? ` in ${activeBackend.name}` : ""} for “{q}”.
              </p>
            )}

          <div className="flex flex-col gap-3">
            {results.map((result) => (
              <ResultCard
                key={`${result.backend}:${result.id}`}
                result={result}
                source={names.get(result.backend) ?? result.backend}
                query={q}
              />
            ))}
          </div>

          {activePending && results.length > 0 && (
            <p className="text-muted-foreground flex items-center justify-center gap-2 text-sm">
              <Spinner /> Waiting for more results…
            </p>
          )}
          {!activePending && hasNextPage && (
            <Button
              variant="outline"
              className="self-center"
              disabled={isFetchingNextPage}
              onClick={() => fetchNextPage()}
            >
              {isFetchingNextPage && <Spinner />}
              Load more
            </Button>
          )}
          {ranking === "fallback" && results.length > 1 && (
            <p className="text-muted-foreground text-center text-xs">
              Results are interleaved in each source&rsquo;s own order, since no
              reranker is configured or it couldn&rsquo;t be reached.
            </p>
          )}
        </>
      )}
    </div>
  );
}

// latestBackends returns the sources' statuses from the most recently fetched
// page: every page lists every source.
function latestBackends(...streams: SearchPage[][]): SearchBackend[] {
  let latest: SearchBackend[] = [];
  let latestKnown = -1;
  for (const pages of streams) {
    const page = pages.at(-1);
    if (!page) continue;
    // A page from later in the search knows of at least as many answers.
    const known = page.backends.filter((b) => b.status !== "pending").length;
    if (known >= latestKnown) {
      latest = page.backends;
      latestKnown = known;
    }
  }
  return latest;
}

function SourceTabs({
  backends,
  total,
  source,
  onSelect,
}: {
  backends: SearchBackend[];
  total: number;
  source: string;
  onSelect: (source: string) => void;
}) {
  return (
    <Tabs value={source} onValueChange={(value) => onSelect(String(value))}>
      <div className="-mx-1 overflow-x-auto px-1">
        <TabsList variant="line">
          <TabsTrigger value={ALL_SOURCES}>
            All
            {source === ALL_SOURCES && total > 0 && (
              <Badge variant="secondary">{total}</Badge>
            )}
          </TabsTrigger>
          {backends.map((b) => (
            <TabsTrigger key={b.id} value={b.id} disabled={!b.cursor}>
              <SourceIcon backend={b} />
              {b.name}
              <SourceState backend={b} />
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
    </Tabs>
  );
}

function SourceIcon({ backend }: { backend: Pick<SearchBackend, "kind"> }) {
  return backend.kind === "records" ? <Database /> : <Plug />;
}

// SourceState shows a source's status in its tab: a spinner while it runs,
// a warning when it failed, its result count once it answered.
function SourceState({ backend }: { backend: SearchBackend }) {
  switch (backend.status) {
    case "pending":
      return <Spinner className="size-3" />;
    case "ok":
      return (
        <Tooltip>
          <TooltipTrigger render={<span />}>
            <Badge variant="secondary">{backend.count}</Badge>
          </TooltipTrigger>
          <TooltipContent>
            {backend.count} {backend.count === 1 ? "result" : "results"}
            {backend.elapsedMs !== undefined &&
              ` in ${formatElapsed(backend.elapsedMs)}`}
          </TooltipContent>
        </Tooltip>
      );
    default:
      return <AlertTriangle className="text-destructive size-3" />;
  }
}

function PendingNotice({ backends }: { backends: SearchBackend[] }) {
  return (
    <div className="bg-muted/50 text-muted-foreground flex items-center gap-2 rounded-xl px-3 py-2 text-sm">
      <Spinner />
      <span>
        Still searching {listNames(backends)}. Results will appear as they
        arrive.
      </span>
    </div>
  );
}

// FailedSources lists the sources that couldn't be searched, and why, so an
// empty or short list isn't mistaken for "nothing there".
function FailedSources({
  backends,
  org,
}: {
  backends: SearchBackend[];
  org: DidString;
}) {
  return (
    <div className="border-destructive/30 bg-destructive/5 flex flex-col gap-2 rounded-xl border px-3 py-2 text-sm">
      {backends.map((b) => (
        <div key={b.id} className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {b.status === "not_connected" ? (
            <Unplug className="text-destructive size-4 shrink-0" />
          ) : (
            <AlertTriangle className="text-destructive size-4 shrink-0" />
          )}
          <span className="font-medium">{b.name}</span>
          <span className="text-muted-foreground">{failureText(b)}</span>
          {b.status === "not_connected" && (
            <Link
              to="/orgs/$org/settings/mcp"
              params={{ org }}
              className="text-primary ml-auto text-xs hover:underline"
            >
              Connect
            </Link>
          )}
        </div>
      ))}
    </div>
  );
}

function failureText(b: SearchBackend): string {
  switch (b.status) {
    case "timeout":
      return "didn’t answer in time.";
    case "not_connected":
      return "isn’t connected, or its sign-in expired.";
    default:
      if (b.errorCode === "interrupted")
        return "was interrupted. Search again to retry it.";
      return b.error ? `failed: ${b.error}` : "failed.";
  }
}

function ResultSkeletons() {
  return (
    <div className="flex flex-col gap-3" aria-label="Loading results">
      {[0, 1, 2].map((i) => (
        <Card key={i}>
          <CardContent className="flex flex-col gap-2 py-4">
            <div className="bg-muted h-4 w-1/3 animate-pulse rounded" />
            <div className="bg-muted h-3 w-full animate-pulse rounded" />
            <div className="bg-muted h-3 w-2/3 animate-pulse rounded" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

function ResultCard({
  result,
  source,
  query,
}: {
  result: SearchResult;
  source: string;
  query: string;
}) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-1.5 py-4">
        <div className="flex items-center gap-2">
          <Badge variant="outline">
            <SourceIcon
              backend={{ kind: result.kind === "record" ? "records" : "mcp" }}
            />
            {source}
          </Badge>
        </div>
        {result.kind === "record" && result.record ? (
          <RecordTitle result={result} space={result.record.space} />
        ) : (
          <ItemTitle result={result} />
        )}
        {result.snippet && (
          <Highlighted
            text={result.snippet}
            query={query}
            className="text-muted-foreground line-clamp-3 text-sm"
          />
        )}
      </CardContent>
    </Card>
  );
}

function RecordTitle({
  result,
  space: spaceUri,
}: {
  result: SearchResult;
  space: string;
}) {
  const uri = result.url ?? result.id;
  const space = SpaceRef.parse(spaceUri);
  // A record URI is its space's URI followed by /<repo>/<collection>/<rkey>.
  const [recordOwner, recordType, recordKey] = uri
    .slice(spaceUri.length + 1)
    .split("/");
  return (
    <div className="flex flex-col">
      <Link
        to="/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner/$recordType/$recordKey"
        params={{
          spaceOwner: space.spaceDid,
          spaceType: space.spaceType,
          spaceKey: space.skey,
          recordOwner,
          recordType,
          recordKey,
        }}
        className="font-mono text-sm font-medium break-all hover:underline"
      >
        {recordType}/{recordKey}
      </Link>
      <span className="text-muted-foreground text-xs break-all">
        {space.spaceType}/{space.skey} · {recordOwner}
      </span>
    </div>
  );
}

function ItemTitle({ result }: { result: SearchResult }) {
  const title = result.title || result.url || "Untitled";
  if (!result.url) return <span className="font-medium">{title}</span>;
  return (
    <div className="flex flex-col">
      <a
        href={result.url}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 font-medium hover:underline"
      >
        <span className="break-words">{title}</span>
        <ExternalLink className="text-muted-foreground size-3 shrink-0" />
      </a>
      <span className="text-muted-foreground text-xs break-all">
        {hostOf(result.url)}
      </span>
    </div>
  );
}

// Highlighted renders text with the query's words marked. Snippets are plain
// text, so they're split rather than rendered as HTML.
function Highlighted({
  text,
  query,
  className,
}: {
  text: string;
  query: string;
  className?: string;
}) {
  const words = query
    .split(/\s+/)
    .filter((w) => w.length > 1)
    .map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  if (words.length === 0) return <p className={className}>{text}</p>;
  // Splitting on a capture group puts each match at an odd index.
  const parts = text.split(new RegExp(`(${words.join("|")})`, "gi"));
  return (
    <p className={className}>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <mark key={i} className="bg-yellow-200/70 text-foreground rounded-sm">
            {part}
          </mark>
        ) : (
          part
        ),
      )}
    </p>
  );
}

function listNames(backends: SearchBackend[]): string {
  const names = backends.map((b) => b.name);
  if (names.length <= 2) return names.join(" and ");
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

function formatElapsed(ms: number): string {
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
