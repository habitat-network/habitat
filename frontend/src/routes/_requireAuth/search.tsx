import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { SpaceRef } from "@atproto/syntax";
import { useState, type FormEvent } from "react";
import { z } from "zod";
import { Button, Card, CardContent, Input } from "internal/components/ui";
import { searchRecordsQueryOptions, type SearchResult } from "@/queries/search";
import { useSelectedOrg } from "@/lib/selectedOrg";

export const Route = createFileRoute("/_requireAuth/search")({
  validateSearch: z.object({
    q: z.string().default(""),
  }),
  component: SearchPage,
});

function SearchPage() {
  const { q } = Route.useSearch();
  const { authManager } = Route.useRouteContext();
  const navigate = Route.useNavigate();
  const [input, setInput] = useState(q);
  // Search one org at a time: the one selected in the header.
  const { org } = useSelectedOrg(authManager);
  const results = useQuery(searchRecordsQueryOptions(authManager, org, q));

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    navigate({ search: { q: input } });
  };

  return (
    <div className="flex flex-col gap-6 py-6">
      <h1 className="text-2xl font-semibold">Search</h1>
      <form onSubmit={onSubmit} className="flex gap-2">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Search records you can read in this community"
        />
        <Button type="submit">Search</Button>
      </form>

      {results.isFetching && (
        <p className="text-muted-foreground text-sm">Searching…</p>
      )}
      {results.isError && (
        <p className="text-destructive text-sm">{results.error.message}</p>
      )}
      {results.data?.length === 0 && (
        <p className="text-muted-foreground text-sm">No matching records.</p>
      )}
      <div className="flex flex-col gap-3">
        {results.data?.map((result) => (
          <ResultCard key={result.uri} result={result} />
        ))}
      </div>
    </div>
  );
}

function ResultCard({ result }: { result: SearchResult }) {
  const space = SpaceRef.parse(result.space);
  // A record URI is its space's URI followed by /<repo>/<collection>/<rkey>.
  const [recordOwner, recordType, recordKey] = result.uri
    .slice(result.space.length + 1)
    .split("/");

  return (
    <Card>
      <CardContent className="flex flex-col gap-1 py-4">
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
          className="font-mono text-sm break-all hover:underline"
        >
          {recordType}/{recordKey}
        </Link>
        <span className="text-muted-foreground text-xs break-all">
          {space.spaceType}/{space.skey} · {recordOwner}
        </span>
        {result.snippet && <Snippet text={result.snippet} />}
      </CardContent>
    </Card>
  );
}

// Snippet renders the server's snippet, whose matches are wrapped in
// <mark></mark> and whose other text is unescaped record text, so it is
// split on the tags rather than rendered as HTML.
function Snippet({ text }: { text: string }) {
  // Splitting on a capture group puts each match at an odd index.
  const parts = text.split(/<mark>(.*?)<\/mark>/);
  return (
    <p className="text-sm">
      {parts.map((part, i) =>
        i % 2 === 1 ? <mark key={i}>{part}</mark> : part,
      )}
    </p>
  );
}
