import { createFileRoute, Link } from "@tanstack/react-router";
import { parseSpaceURI } from "internal";
import {
  Card,
  CardContent,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import { spacesListQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb, shortenDid } from "@/components/SpacesBreadcrumb";

export const Route = createFileRoute("/_requireAuth/spaces/type/$spaceType")({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      spacesListQueryOptions(context.authManager),
    ),
  pendingComponent: () => <p className="py-8">Loading spaces…</p>,
  component: SpacesOfType,
});

function SpacesOfType() {
  const { spaceType } = Route.useParams();
  const allSpaces = Route.useLoaderData();

  // The full list is already cached by the spaces landing page, so filtering
  // in place avoids a second round trip for what is the same data.
  const spaces = allSpaces
    .filter((space) => space.type === spaceType)
    .map((space) => ({ ...space, parts: parseSpaceURI(space.uri) }));

  return (
    <div className="flex flex-col gap-6 py-6">
      <SpacesBreadcrumb spaceType={spaceType} />

      <div>
        <h1 className="text-2xl font-semibold font-mono">{spaceType}</h1>
        <p className="text-muted-foreground text-sm">
          {spaces.length} space{spaces.length === 1 ? "" : "s"} of this type.
        </p>
      </div>

      {spaces.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            No spaces of this type.
          </CardContent>
        </Card>
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Space key</TableHead>
                <TableHead>Owner</TableHead>
                <TableHead className="text-right">Members</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {spaces.map((space) => (
                <TableRow key={space.uri}>
                  <TableCell className="font-mono">
                    {space.parts ? (
                      <Link
                        to="/spaces/$spaceOwner/$spaceType/$spaceKey"
                        params={space.parts}
                        className="hover:underline"
                      >
                        {space.parts.spaceKey}
                      </Link>
                    ) : (
                      space.uri
                    )}
                  </TableCell>
                  <TableCell
                    className="font-mono text-xs text-muted-foreground"
                    title={space.parts?.spaceOwner}
                  >
                    {space.parts ? shortenDid(space.parts.spaceOwner) : "—"}
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-muted-foreground">
                    {space.memberCount ?? 0}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </div>
  );
}
