import { createFileRoute } from "@tanstack/react-router";
import { spacesListQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesTable } from "@/components/SpacesTable";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute("/_requireAuth/spaces/type/$spaceType")({
  loader: ({ context, params }) =>
    context.queryClient.fetchQuery(
      spacesListQueryOptions(context.authManager, { type: params.spaceType }),
    ),
  component: SpacesOfType,
});

function SpacesOfType() {
  const { spaceType } = Route.useParams();
  const spaces = Route.useLoaderData();
  return (
    <SpacesPageLayout
      breadcrumb={<SpacesBreadcrumb spaceType={spaceType} />}
      title={<span className="font-mono break-all">{spaceType}</span>}
      subtitle={
        <>
          {spaces.length} space{spaces.length === 1 ? "" : "s"} of this type,
          across every owner.
        </>
      }
    >
      <SpacesTable
        spaces={spaces}
        showOwner
        emptyMessage="No spaces of this type."
      />
    </SpacesPageLayout>
  );
}
