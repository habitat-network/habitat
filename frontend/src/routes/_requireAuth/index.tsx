import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";
import {
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Item,
  ItemGroup,
  ItemHeader,
  ItemTitle,
} from "internal/components/ui";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { CollectionCard } from "@/components/CollectionCard";
import { App } from "api/types/network/habitat/listConnectedApps";

import { Search } from "lucide-react";

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const { authManager } = context;
    const appData = await query(
      "network.habitat.listConnectedApps",
      {},
      { authManager },
    );

    // List collections for manage your data preview
    const data = await query(
      "network.habitat.repo.listCollections",
      {},
      { authManager },
    );
    const collections = data.collections.slice(0, 3); // Just show the first three in the preview

    const apps = appData.apps.filter(
      (app) => app.clientUri !== `https://${__DOMAIN__}`,
    );
    return { collections, apps: apps };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    return <AuthenticatedHome />;
  },
});

interface RecentlyUsedProps {
  apps: App[];
}

function RecentlyUsed({ apps }: RecentlyUsedProps) {
  return (
    <Card size="sm" className="flex-1 min-w-128">
      <CardHeader>
        <CardTitle>Recently used</CardTitle>
      </CardHeader>
      <CardContent>
        <ItemGroup className="grid grid-cols-3">
          {apps.map((app) => (
            <Item
              key={app.clientID}
              render={<Link to={app.clientUri} />}
              variant="muted"
            >
              <ItemHeader className="rounded bg-background p-2">
                {app.logoUri ? (
                  <img
                    src={app.logoUri}
                    alt={app.name}
                    className="w-12 h-12 object-contain mx-auto"
                  />
                ) : null}
              </ItemHeader>
              <ItemTitle className="text-xs text-center truncate w-full px-1">
                {app.name}
              </ItemTitle>
            </Item>
          ))}
        </ItemGroup>
      </CardContent>
    </Card>
  );
}

interface ManageDataPreviewProps {
  collections: CollectionMetadata[];
}

function ManageDataPreview({ collections }: ManageDataPreviewProps) {
  const { authManager } = Route.useRouteContext();
  return (
    <Card size="sm" className="flex-1 min-w-128">
      <CardHeader>
        <CardTitle>Manage your data</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid">
          {collections.map((collection) => {
            return (
              <CollectionCard
                key={collection.nsid}
                authManager={authManager}
                collection={collection}
              />
            );
          })}
        </div>
      </CardContent>
      <CardFooter>
        <Button
          variant="ghost"
          className="w-full"
          render={<Link to="/collections" className="text-sm !no-underline" />}
        >
          See all →
        </Button>
      </CardFooter>
    </Card>
  );
}

function AuthenticatedHome() {
  const { collections, apps } = Route.useLoaderData()!;

  // For now, don't require the user to be registered with a habitat service. If they do have one,
  // requests will still be routed there, but allow them to use the centralized one by default.

  return (
    <>
      <div className="flex-1 flex flex-col gap-4 justify-center min-h-[60vh]">
        <h1 className="text-2xl">Welcome to Habitat!</h1>
        <InputGroup>
          <InputGroupInput
            disabled
            placeholder="Search your data for anything... coming soon!"
          />
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
        </InputGroup>
      </div>

      <div className="flex gap-4 flex-wrap">
        <RecentlyUsed apps={apps} />
        <ManageDataPreview collections={collections} />
      </div>
    </>
  );
}
