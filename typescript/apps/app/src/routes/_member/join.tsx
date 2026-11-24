import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { OrgAvatar } from "internal";
import {
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
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { myGroupsQuery, myInvitesQuery } from "@/lib/queries";
import { acceptInvite } from "@/server/fns/groups";

// Join a group: the member's pending invites.

export const Route = createFileRoute("/_member/join")({
  component: JoinPage,
});

function JoinPage() {
  const navigate = useNavigate({ from: Route.fullPath });
  const queryClient = useQueryClient();
  const { data: invites = [] } = useQuery(myInvitesQuery);
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

      </PageBody>
    </>
  );
}
