import { createFileRoute } from "@tanstack/react-router";
import { spacesListQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesTable } from "@/components/SpacesTable";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";
import { parseSpaceURI } from "internal";

export const Route = createFileRoute("/_requireAuth/spaces/$spaceOwner/")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      spacesListQueryOptions(context.authManager, { did: params.spaceOwner }),
    ),
  component: SpacesByOwner,
});

function SpacesByOwner() {
  const { spaceOwner } = Route.useParams();
  const spaces = Route.useLoaderData();

  const typeCount = new Set(
    spaces.map((space) => parseSpaceURI(space.uri)?.spaceType),
  ).size;

  return (
    <SpacesPageLayout
      breadcrumb={<SpacesBreadcrumb spaceOwner={spaceOwner} />}
      title={<span className="font-mono break-all">{spaceOwner}</span>}
      subtitle={
        <>
          {spaces.length} space{spaces.length === 1 ? "" : "s"} owned by this
          account across {typeCount} type{typeCount === 1 ? "" : "s"}.
        </>
      }
    >
      <SpacesTable
        spaces={spaces}
        showType
        emptyMessage="This account owns no spaces you can see."
      />
    </SpacesPageLayout>
  );
}
