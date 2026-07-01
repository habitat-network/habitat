import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";
import {
  Card,
  CardContent,
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

    const apps = appData.apps.filter(
      (app) => app.clientUri !== `https://${__DOMAIN__}`,
    );

    let orgName: string | undefined;
    try {
      const meta = await query(
        "network.habitat.org.getMetadata",
        {},
        { authManager },
      );
      orgName = meta.name;
    } catch {
      // Not a member of an org
    }

    return { apps, orgName };
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

function AuthenticatedHome() {
  const { apps } = Route.useLoaderData()!;

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
      </div>
    </>
  );
}
