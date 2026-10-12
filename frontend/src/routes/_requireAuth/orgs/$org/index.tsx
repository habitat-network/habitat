import { createFileRoute, Link } from "@tanstack/react-router";
import { xrpc } from "@atproto/lex";
import { network } from "api";
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
import Avatar from "boring-avatars";

import { Search } from "lucide-react";
import { useState, type FormEvent } from "react";

export const Route = createFileRoute("/_requireAuth/orgs/$org/")({
  async loader({ context }) {
    const { authManager } = context;
    const appData = await xrpc(
      authManager,
      network.habitat.listConnectedApps.main,
      { params: {} },
    ).catch(() => ({ body: { apps: [] } }));

    const apps = appData.body.apps.filter(
      (app) => app.clientUri !== import.meta.env.VITE_BASE_URL,
    );

    return {
      apps,
    };
  },
  component() {
    return <AuthenticatedHome />;
  },
});

interface RecentlyUsedProps {
  apps: network.habitat.listConnectedApps.App[];
}

function RecentlyUsed({ apps }: RecentlyUsedProps) {
  return (
    <Card size="sm" className="flex-1 min-w-128">
      <CardHeader>
        <CardTitle>Recently used</CardTitle>
      </CardHeader>
      <CardContent>
        <ItemGroup className="grid grid-cols-3">
          {apps
            .filter((app) => Boolean(app.clientUri))
            .map((app) => (
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
                  ) : (
                    <Avatar
                      className="mx-auto"
                      name={app.clientID}
                      variant="sunset"
                      square
                    />
                  )}
                </ItemHeader>
                <ItemTitle className="text-xs text-center truncate w-full px-1">
                  {app.name || app.clientID || app.clientUri}
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
  const { org } = Route.useParams();
  const navigate = Route.useNavigate();
  const [q, setQ] = useState("");

  const onSearch = (e: FormEvent) => {
    e.preventDefault();
    if (q.trim())
      navigate({ to: "/orgs/$org/search", params: { org }, search: { q } });
  };

  // For now, don't require the user to be registered with a habitat service. If they do have one,
  // requests will still be routed there, but allow them to use the centralized one by default.

  return (
    <>
      <div className="flex-1 flex flex-col gap-4 justify-center min-h-[60vh]">
        <h1 className="text-2xl">Welcome to Habitat!</h1>
        <form onSubmit={onSearch}>
          <InputGroup>
            <InputGroupInput
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search your data for anything..."
            />
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
          </InputGroup>
        </form>
      </div>
      {apps.length > 0 ? (
        <div className="flex gap-4 flex-wrap">
          <RecentlyUsed apps={apps} />
        </div>
      ) : (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            Looks like there&rsquo;s nothing here!{" "}
            <a
              href="https://habitat.network/habitat/api/docs/habitat"
              target="_blank"
              rel="noopener noreferrer"
              className="underline text-primary"
            >
              Read the docs
            </a>{" "}
            to get started building.
          </CardContent>
        </Card>
      )}
    </>
  );
}
