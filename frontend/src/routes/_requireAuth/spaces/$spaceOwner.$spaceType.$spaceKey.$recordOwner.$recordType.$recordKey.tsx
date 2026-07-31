import { createFileRoute } from "@tanstack/react-router";
import ReactJson from "react-json-view";
import { constructSpaceURI } from "internal";
import { Card, CardContent } from "internal/components/ui";
import { spaceRecordQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

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
    <SpacesPageLayout
      breadcrumb={
        <SpacesBreadcrumb
          spaceOwner={spaceOwner}
          spaceType={spaceType}
          spaceKey={spaceKey}
          recordOwner={recordOwner}
          record={{ recordType, recordKey }}
        />
      }
      title={<span className="font-mono break-all">{recordKey}</span>}
      subtitle={
        <>
          <p className="font-mono">{recordType}</p>
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
        </>
      }
    >
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
    </SpacesPageLayout>
  );
}
