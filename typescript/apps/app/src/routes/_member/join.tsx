import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { OrgAvatar } from "internal";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { Search } from "lucide-react";
import { useState } from "react";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { myGroupsQuery, myInvitesQuery } from "@/lib/queries";
import { acceptInvite, lookupGroup } from "@/server/fns/groups";

// Join a group: the member's pending invites, and looking a group up by its
// handle to see its profile and join it.

export const Route = createFileRoute("/_member/join")({
  validateSearch: z.object({ group: z.string().optional() }),
  component: JoinPage,
});

const POLICY_LABELS = {
  open: "Open",
  invite: "Invite only",
  request: "By request",
} as const;

function JoinPage() {
  const { group: lookup } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(lookup ?? "");
  const { data: invites = [] } = useQuery(myInvitesQuery);
  const { data: groups = [] } = useQuery(myGroupsQuery);
  const found = useQuery({
    queryKey: ["lookupGroup", lookup],
    enabled: !!lookup,
    queryFn: () => lookupGroup({ data: { group: lookup ?? "" } }),
    retry: false,
  });
  const join = useMutation({
    mutationFn: (org: string) => acceptInvite({ data: { org } }),
    onSuccess: async (_, org) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: myGroupsQuery.queryKey }),
        queryClient.invalidateQueries({ queryKey: myInvitesQuery.queryKey }),
      ]);
      await navigate({ to: "/g/$org/files", params: { org } });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't join",
        description: e.message,
      }),
  });
  const g = found.data;
  const alreadyIn = g ? groups.some((x) => x.did === g.did) : false;
  const invited = g ? invites.some((i) => i.org === g.did) : false;

  return (
    <>
      <PageHeader title="Join a group" />
      <PageBody className="flex max-w-3xl flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Your invites</CardTitle>
            <CardDescription>Groups that have invited you.</CardDescription>
          </CardHeader>
          <CardContent>
            {invites.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No pending invites.
              </p>
            ) : (
              <ItemGroup className="gap-2">
                {invites.map((i) => (
                  <Item key={i.id} variant="outline">
                    <ItemMedia>
                      <OrgAvatar did={i.org} name={i.orgName ?? undefined} />
                    </ItemMedia>
                    <ItemContent>
                      <ItemTitle>{i.orgName ?? i.org}</ItemTitle>
                      <ItemDescription>
                        Join as {i.roles.join(", ") || "member"}
                      </ItemDescription>
                    </ItemContent>
                    <ItemActions>
                      <Button
                        disabled={join.isPending}
                        onClick={() => join.mutate(i.org)}
                      >
                        Accept
                      </Button>
                    </ItemActions>
                  </Item>
                ))}
              </ItemGroup>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Find a group</CardTitle>
            <CardDescription>
              Look a group up by its handle to see its profile.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void navigate({ search: { group: draft.trim() || undefined } });
              }}
            >
              <InputGroup>
                <InputGroupAddon>
                  <Search />
                </InputGroupAddon>
                <InputGroupInput
                  placeholder="mygroup.example.com"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupButton type="submit">Look up</InputGroupButton>
                </InputGroupAddon>
              </InputGroup>
            </form>
            {found.isError && (
              <p className="text-sm text-destructive">{found.error.message}</p>
            )}
            {g && (
              <Card className="bg-muted/30">
                <CardHeader className="flex-row items-center gap-3">
                  <OrgAvatar did={g.did} name={g.name ?? undefined} size="lg" />
                  <div className="min-w-0">
                    <CardTitle>{g.name ?? g.handle ?? g.did}</CardTitle>
                    <CardDescription className="truncate">
                      {g.handle ? `@${g.handle}` : g.did}
                    </CardDescription>
                  </div>
                  {g.joinPolicy && (
                    <Badge variant="secondary" className="ml-auto">
                      {POLICY_LABELS[g.joinPolicy]}
                    </Badge>
                  )}
                </CardHeader>
                {g.description && (
                  <CardContent>
                    <p className="text-sm whitespace-pre-wrap">
                      {g.description}
                    </p>
                  </CardContent>
                )}
                <CardFooter className="justify-end">
                  {alreadyIn ? (
                    <Button
                      variant="outline"
                      onClick={() =>
                        navigate({
                          to: "/g/$org/files",
                          params: { org: g.did },
                        })
                      }
                    >
                      Open group
                    </Button>
                  ) : invited || g.joinPolicy === "open" ? (
                    <Button
                      disabled={join.isPending}
                      onClick={() => join.mutate(g.did)}
                    >
                      Join {g.name ?? "group"}
                    </Button>
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      {g.joinPolicy === "request"
                        ? "This group approves new members. Ask an admin to invite you."
                        : "You need an invite to join this group."}
                    </p>
                  )}
                </CardFooter>
              </Card>
            )}
          </CardContent>
        </Card>
      </PageBody>
    </>
  );
}
