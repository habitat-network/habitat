import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { OrgAvatar, UserAvatar } from "internal";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { AppWindow } from "lucide-react";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { groupLabel, myGroupsQuery } from "@/lib/queries/groups";
import { connectedAppsQuery } from "@/lib/queries/mcp";
import { signOut } from "@/server/fns/auth";
import { leaveGroup } from "@/server/fns/groups";

// Connections: the member's account, the groups they belong to, and the
// apps they've authorized to act for them.

export const Route = createFileRoute("/_member/connections")({
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(myGroupsQuery),
      context.queryClient.ensureQueryData(connectedAppsQuery),
    ]),
  component: ConnectionsPage,
});

const dateFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
});

function ConnectionsPage() {
  const { member } = Route.useRouteContext();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: groups } = useSuspenseQuery(myGroupsQuery);
  const { data: apps } = useSuspenseQuery(connectedAppsQuery);
  const leave = useMutation({
    mutationFn: (org: string) => leaveGroup({ data: { org } }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: myGroupsQuery.queryKey }),
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't leave the group",
        description: e.message,
      }),
  });

  return (
    <>
      <PageHeader
        title="Connections"
        description="Your account, groups and apps"
      />
      <PageBody className="flex max-w-3xl flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Account</CardTitle>
          </CardHeader>
          <CardContent>
            <Item variant="outline">
              <ItemMedia>
                <UserAvatar
                  actor={{
                    did: member.did,
                    handle: member.handle ?? undefined,
                  }}
                />
              </ItemMedia>
              <ItemContent>
                <ItemTitle>
                  {member.handle ? `@${member.handle}` : member.did}
                </ItemTitle>
                <ItemDescription className="break-all">
                  {member.did}
                </ItemDescription>
              </ItemContent>
              <ItemActions>
                <Button
                  variant="outline"
                  onClick={async () => {
                    await signOut();
                    queryClient.clear();
                    await navigate({ to: "/login" });
                  }}
                >
                  Sign out
                </Button>
              </ItemActions>
            </Item>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Groups</CardTitle>
            <CardDescription>
              Groups you belong to. A connected group can hold docs and files in
              this app.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ItemGroup className="gap-2">
              {groups.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  You're not in any groups yet.{" "}
                  <Link to="/join" className="underline">
                    Join one
                  </Link>
                  .
                </p>
              )}
              {groups.map((g) => (
                <Item key={g.did} variant="outline">
                  <ItemMedia>
                    <OrgAvatar did={g.did} name={g.name ?? undefined} />
                  </ItemMedia>
                  <ItemContent>
                    <ItemTitle>
                      <Link
                        to="/g/$org/files"
                        params={{ org: g.did }}
                        className="hover:underline"
                      >
                        {groupLabel(g)}
                      </Link>
                      <Badge variant={g.connected ? "default" : "outline"}>
                        {g.connected ? "Connected" : "Not connected"}
                      </Badge>
                    </ItemTitle>
                    <ItemDescription className="break-all">
                      {g.did}
                    </ItemDescription>
                  </ItemContent>
                  <ItemActions>
                    <Button
                      variant="ghost"
                      disabled={leave.isPending}
                      onClick={() => leave.mutate(g.did)}
                    >
                      Leave
                    </Button>
                  </ItemActions>
                </Item>
              ))}
            </ItemGroup>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Authorized apps</CardTitle>
            <CardDescription>
              Apps you've signed in to with your Habitat account.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ItemGroup className="gap-2">
              {apps.length === 0 && (
                <p className="text-sm text-muted-foreground">No apps yet.</p>
              )}
              {apps.map((a) => (
                <Item key={a.clientID} variant="outline">
                  <ItemMedia variant="icon">
                    {a.logoUri ? (
                      <img src={a.logoUri} alt="" className="size-5" />
                    ) : (
                      <AppWindow />
                    )}
                  </ItemMedia>
                  <ItemContent>
                    <ItemTitle>{a.name}</ItemTitle>
                    <ItemDescription className="break-all">
                      {a.clientUri} · last used{" "}
                      {dateFormat.format(new Date(a.lastUsed))}
                    </ItemDescription>
                  </ItemContent>
                </Item>
              ))}
            </ItemGroup>
          </CardContent>
        </Card>
      </PageBody>
    </>
  );
}
