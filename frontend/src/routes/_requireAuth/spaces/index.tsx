import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/spaces/")({
  async loader({ context }) {
    const { authManager } = context;
    const data = await query(
      "network.habitat.space.listSpaces",
      {},
      { authManager },
    );
    return { spaces: data.spaces };
  },
  pendingComponent: () => <p>Loading spaces...</p>,
  component: SpacesList,
});

function SpacesList() {
  const { spaces } = Route.useLoaderData();

  return (
    <div className="p-4">
      <h1 className="text-2xl mb-4">Spaces</h1>
      <div className="grid !grid-cols-1 sm:!grid-cols-2 lg:!grid-cols-3 gap-4">
        {spaces.map((space) => (
          <Link
            key={space.uri}
            to="/spaces/$space"
            params={{ space: encodeURIComponent(space.uri) }}
          >
            <Card className="hover:bg-accent transition-colors cursor-pointer">
              <CardHeader>
                <CardTitle className="text-sm font-mono truncate">
                  {space.skey ?? space.uri.split("/").pop()}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-xs text-muted-foreground space-y-1">
                  <p>Type: {space.type}</p>
                  <p>Members: {space.memberCount ?? 0}</p>
                  <p className="truncate">URI: {space.uri}</p>
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}
