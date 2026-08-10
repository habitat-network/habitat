import { useState } from "react";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { constructSpaceURI, procedure, type AuthManager } from "internal";
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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
import {
  spaceLatestCommitQueryOptions,
  spaceRecordsQueryOptions,
  type SpaceCommit,
  type SpaceRecord,
} from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { DidHoverCard } from "@/components/DidHoverCard";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner/",
)({
  async loader({ context, params }) {
    const { spaceOwner, spaceType, spaceKey, recordOwner } = params;
    const space = constructSpaceURI({ spaceOwner, spaceType, spaceKey });
    const [records, commit] = await Promise.all([
      context.queryClient.fetchQuery(
        spaceRecordsQueryOptions(space, recordOwner, context.authManager),
      ),
      context.queryClient.fetchQuery(
        spaceLatestCommitQueryOptions(space, recordOwner, context.authManager),
      ),
    ]);
    return { records, commit };
  },
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

  const { records, commit } = Route.useLoaderData();
  const collections = groupByCollection(records);

  return (
    <SpacesPageLayout
      breadcrumb={
        <SpacesBreadcrumb
          spaceOwner={spaceOwner}
          spaceType={spaceType}
          spaceKey={spaceKey}
          recordOwner={recordOwner}
        />
      }
      title={
        <DidHoverCard did={recordOwner} className="font-mono break-all">
          {recordOwner}
        </DidHoverCard>
      }
      subtitle={
        <>
          {records.length} record{records.length === 1 ? "" : "s"} across{" "}
          {collections.length} collection
          {collections.length === 1 ? "" : "s"} in this space.
        </>
      }
    >
      <LatestCommitCard commit={commit} />

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
    </SpacesPageLayout>
  );
}

// LatestCommitCard shows the host-signed commit over this repo's state in the
// space: the revision it covers, and the raw crypto material behind it.
function LatestCommitCard({ commit }: { commit: SpaceCommit | null }) {
  if (!commit) {
    return (
      <Card>
        <CardContent className="py-6 text-center text-sm text-muted-foreground">
          No signed commit for this repo in this space yet.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Latest commit</CardTitle>
        <CardDescription>
          Revision <span className="font-mono">{commit.rev}</span>, commit
          format v{commit.ver}.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-2 text-xs">
          <CommitField label="Hash" value={commit.hash} />
          <CommitField label="MAC" value={commit.mac} />
          <CommitField label="IKM" value={commit.ikm} />
          <CommitField label="Signature" value={commit.sig} />
        </dl>
      </CardContent>
    </Card>
  );
}

// CommitField renders one base64 field of the commit. The values are long
// enough to wrap over several lines, so they truncate with the full value in a
// tooltip.
function CommitField({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="truncate font-mono" title={value}>
        {value}
      </dd>
    </>
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
      // Deleting a record moves the repo's head, so the signed commit shown
      // above the collections is stale too.
      await Promise.all([
        queryClient.invalidateQueries(
          spaceRecordsQueryOptions(space, params.recordOwner, authManager),
        ),
        queryClient.invalidateQueries(
          spaceLatestCommitQueryOptions(space, params.recordOwner, authManager),
        ),
      ]);
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
