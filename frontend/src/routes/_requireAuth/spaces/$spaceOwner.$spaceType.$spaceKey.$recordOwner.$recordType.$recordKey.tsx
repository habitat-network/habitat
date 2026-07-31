import { createFileRoute } from "@tanstack/react-router";
import ReactJson from "react-json-view";
import { constructSpaceURI } from "internal";
import { Card, CardContent } from "internal/components/ui";
import { spaceRecordQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner/$recordType/$recordKey",
)({
  loader({ context, params }) {
    const { spaceOwner, spaceType, spaceKey, recordOwner } = params;
    return context.queryClient.ensureQueryData(
      spaceRecordQueryOptions(
        {
          space: constructSpaceURI({ spaceOwner, spaceType, spaceKey }),
          repo: recordOwner,
          collection: params.recordType,
          rkey: params.recordKey,
        },
        context.authManager,
      ),
    );
  },
  pendingComponent: () => <p className="py-8">Loading record…</p>,
  component: RecordView,
});

function RecordView() {
  const {
    spaceOwner,
    spaceType,
    spaceKey,
    recordOwner,
    recordType,
    recordKey,
  } = Route.useParams();
  const data = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-6 py-6">
      <SpacesBreadcrumb
        space={{ spaceOwner, spaceType, spaceKey }}
        recordOwner={recordOwner}
        record={{ recordType, recordKey }}
      />

      <div>
        <h1 className="text-2xl font-semibold font-mono break-all">
          {recordKey}
        </h1>
        <p className="text-muted-foreground text-sm font-mono">{recordType}</p>
        {data.cid && (
          // CIDs are long enough to wrap onto several lines and swamp the
          // header, so they get their own truncated line.
          <p
            className="max-w-full truncate text-xs text-muted-foreground/70 font-mono"
            title={data.cid}
          >
            {data.cid}
          </p>
        )}
      </div>

      <Card>
        <CardContent className="overflow-x-auto">
          <ReactJson
            src={(data.value ?? {}) as object}
            name={false}
            displayDataTypes={false}
            enableClipboard={false}
          />
        </CardContent>
      </Card>
    </div>
  );
}
