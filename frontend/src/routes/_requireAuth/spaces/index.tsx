import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { procedure, query } from "internal";
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Input,
} from "internal/components/ui";
import { useState } from "react";

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
  const { authManager } = Route.useRouteContext();
  const router = useRouter();

  const [spaceType, setSpaceType] = useState<string>("");

  return (
    <div className="p-4">
      <h1 className="text-2xl mb-4">Spaces</h1>
      <div className="flex gap-2 my-4">
        <Input
          value={spaceType}
          onChange={(e) => setSpaceType(e.target.value)}
          placeholder="Space type"
        />
        <Button
          onClick={async () => {
            const { uri } = await procedure(
              "network.habitat.space.createSpace",
              {
                type: spaceType,
              },
              { authManager },
            );
            await router.navigate({
              to: "/spaces/$space",
              params: {
                space: uri,
              },
            });
          }}
        >
          Create space
        </Button>
      </div>
      <div className="grid !grid-cols-1 sm:!grid-cols-2 lg:!grid-cols-3 gap-4">
        {spaces.map((space) => (
          <Link
            key={space.uri}
            to="/spaces/$space"
            params={{ space: space.uri }}
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
