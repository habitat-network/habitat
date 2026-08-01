import { createFileRoute } from "@tanstack/react-router";
import { spacesListQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesTable } from "@/components/SpacesTable";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/",
)({
  loader: ({ context, params }) =>
    context.queryClient.fetchQuery(
      spacesListQueryOptions(context.authManager, {
        did: params.spaceOwner,
        type: params.spaceType,
      }),
    ),
  component: SpacesByOwnerAndType,
});

function SpacesByOwnerAndType() {
  const { spaceOwner, spaceType } = Route.useParams();
  const spaces = Route.useLoaderData();

  return (
    <SpacesPageLayout
      breadcrumb={
        <SpacesBreadcrumb spaceOwner={spaceOwner} spaceType={spaceType} />
      }
      title={<span className="font-mono break-all">{spaceType}</span>}
      subtitle={
        <>
          {spaces.length} space{spaces.length === 1 ? "" : "s"} of this type
          owned by <span className="font-mono break-all">{spaceOwner}</span>.
        </>
      }
    >
      <SpacesTable
        spaces={spaces}
        emptyMessage="This account owns no spaces of this type."
      />
    </SpacesPageLayout>
  );
}
