import { useState } from "react";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { constructSpaceURI, procedure, type AuthManager } from "internal";
import {
  Button,
  Card,
  CardContent,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
} from "internal/components/ui";
import { ChevronRight, X } from "lucide-react";
import { spaceRecordsQueryOptions, type SpaceRecord } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner/",
)({
  loader({ context, params }) {
    const { spaceOwner, spaceType, spaceKey, recordOwner } = params;
    return context.queryClient.ensureQueryData(
      spaceRecordsQueryOptions(
        constructSpaceURI({ spaceOwner, spaceType, spaceKey }),
        recordOwner,
        context.authManager,
      ),
    );
  },
  pendingComponent: () => <p className="py-8">Loading records…</p>,
  component: MemberRecords,
});

// groupByCollection buckets a member's records into one collapsible section
// per collection, ordered by NSID.
function groupByCollection(records: SpaceRecord[]): [string, SpaceRecord[]][] {
  const byCollection = new Map<string, SpaceRecord[]>();
  for (const record of records) {
    const bucket = byCollection.get(record.collection);
    if (bucket) bucket.push(record);
    else byCollection.set(record.collection, [record]);
  }
  return [...byCollection.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function MemberRecords() {
  const { spaceOwner, spaceType, spaceKey, recordOwner } = Route.useParams();
  const space = constructSpaceURI({ spaceOwner, spaceType, spaceKey });
  const { authManager } = Route.useRouteContext();

  const records = Route.useLoaderData();
  const collections = groupByCollection(records);

  return (
    <div className="flex flex-col gap-6 py-6">
      <SpacesBreadcrumb
        space={{ spaceOwner, spaceType, spaceKey }}
        recordOwner={recordOwner}
      />

      <div>
        <h1 className="text-2xl font-semibold font-mono break-all">
          {recordOwner}
        </h1>
        <p className="text-muted-foreground text-sm">
          {records.length} record{records.length === 1 ? "" : "s"} across{" "}
          {collections.length} collection
          {collections.length === 1 ? "" : "s"} in this space.
        </p>
      </div>

      {collections.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            This member has no records in the space.
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-2">
          {collections.map(([collection, collectionRecords]) => (
            <CollectionSection
              key={collection}
              collection={collection}
              records={collectionRecords}
              space={space}
              authManager={authManager}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function CollectionSection({
  collection,
  records,
  space,
  authManager,
}: {
  collection: string;
  records: SpaceRecord[];
  space: string;
  authManager: AuthManager;
}) {
  const params = Route.useParams();
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const router = useRouter();

  const { mutate: deleteRecord } = useMutation({
    async mutationFn(rkey: string) {
      await procedure(
        "network.habitat.space.deleteRecord",
        { space, collection, rkey, repo: params.recordOwner },
        { authManager },
      );
    },
    async onSuccess() {
      await queryClient.invalidateQueries(
        spaceRecordsQueryOptions(space, params.recordOwner, authManager),
      );
      await router.invalidate();
    },
    onError(error) {
      // deleteRecord requires the space-owner relation, so a plain member's
      // delete is rejected. Say so rather than appearing to do nothing.
      toast.add({
        title: "Couldn't delete record",
        description: error.message,
      });
    },
  });

  // Only the signed-in user's own records can be deleted from here.
  const canDelete = authManager.getAuthInfo()?.did === params.recordOwner;

  return (
    <Card className="p-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex w-full items-center gap-2 px-3 py-3 text-left transition-colors hover:bg-muted/50">
          <ChevronRight
            className={`size-4 shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <span className="font-mono text-sm">{collection}</span>
          <span className="ml-auto text-xs text-muted-foreground tabular-nums">
            {records.length}
          </span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Record key</TableHead>
                <TableHead>CID</TableHead>
                {canDelete && <TableHead className="w-0" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((record) => (
                <TableRow key={record.rkey}>
                  <TableCell className="font-mono">
                    <Link
                      to="/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner/$recordType/$recordKey"
                      params={{
                        ...params,
                        recordType: collection,
                        recordKey: record.rkey,
                      }}
                      className="hover:underline"
                    >
                      {record.rkey}
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {/* max-width has no effect on a td, so the ellipsis has to
                        live on a block-level child. */}
                    <span
                      className="block max-w-[24rem] truncate"
                      title={record.cid}
                    >
                      {record.cid}
                    </span>
                  </TableCell>
                  {canDelete && (
                    <TableCell>
                      <Button
                        variant="destructive"
                        size="icon-xs"
                        aria-label={`Delete ${record.rkey}`}
                        onClick={() => deleteRecord(record.rkey)}
                      >
                        <X />
                      </Button>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}
