import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import {
  Badge,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import ReactJson from "react-json-view";
import { RecordRenderer } from "@/components/RecordRenderer";
import {
  collectionRecordsQueryOptions,
  recordBodyQueryOptions,
  type RecordView,
} from "@/queries/collections";

export const Route = createFileRoute("/_requireAuth/collections/$collection")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      collectionRecordsQueryOptions(params.collection, context.authManager),
    ),
  pendingComponent: () => {
    const { collection } = Route.useParams();
    return <p className="py-8">Loading {collection}…</p>;
  },
  component: CollectionRecords,
});

// spaceLabel shortens a space URI (ats://did/type/skey) to its skey for display.
function spaceLabel(uri: string): string {
  const parts = uri.split("/");
  return parts[parts.length - 1] || uri;
}

function CollectionRecords() {
  const { collection } = Route.useParams();
  const records = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();

  return (
    <div className="flex flex-col gap-4 py-6">
      <div>
        <h1 className="text-2xl font-semibold break-all">{collection}</h1>
        <p className="text-muted-foreground text-sm">
          {records.length} {records.length === 1 ? "record" : "records"} you can
          see.
        </p>
      </div>
      {records.length === 0 ? (
        <p className="text-muted-foreground">No records to show.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Record</TableHead>
              <TableHead>Spaces</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <TableRow key={record.uri}>
                <TableCell>
                  <RecordBody
                    record={record}
                    collection={collection}
                    authManager={authManager}
                  />
                </TableCell>
                <TableCell className="align-top">
                  <div className="flex flex-wrap gap-1">
                    {record.spaces.map((space) => (
                      <Badge key={space} variant="outline">
                        {spaceLabel(space)}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

// RecordBody fetches the record's body on demand from pear and renders it. The
// collections index only stores record identity, not bodies.
function RecordBody({
  record,
  collection,
  authManager,
}: {
  record: RecordView;
  collection: string;
  authManager: AuthManager;
}) {
  const { data, isLoading, error } = useQuery(
    recordBodyQueryOptions(record, authManager),
  );

  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs text-muted-foreground break-all">
        {record.repo} · {record.rkey}
      </span>
      {isLoading ? (
        <span className="text-sm text-muted-foreground">Loading record…</span>
      ) : error ? (
        <span className="text-sm text-destructive">
          Couldn’t load record: {(error as Error).message}
        </span>
      ) : data && typeof data === "object" ? (
        <RecordRenderer
          record={data as Record<string, unknown>}
          lexicon={collection}
          uri={record.uri}
        />
      ) : (
        <ReactJson src={{ value: data }} collapsed={1} />
      )}
    </div>
  );
}
