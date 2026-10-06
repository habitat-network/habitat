import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import type { AuthManager } from "internal";
import { OrgAvatar } from "internal";
import {
  acceptInvite,
  myInvitesQueryOptions,
  myOrgsQueryOptions,
  orgProfileQueryOptions,
  type InviteView,
} from "@/queries/opensocial";
import { DidHoverCard } from "@/components/DidHoverCard";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export function PendingInvites({
  invites,
  authManager,
}: {
  invites: InviteView[];
  authManager: AuthManager;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          Pending invites ({invites.length})
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead>Roles</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {invites.map((invite) => (
              <PendingInviteRow
                key={`${invite.org}:${invite.id}`}
                invite={invite}
                authManager={authManager}
              />
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function PendingInviteRow({
  invite,
  authManager,
}: {
  invite: InviteView;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const router = useRouter();

  const { data: profile } = useQuery(
    orgProfileQueryOptions(invite.org, authManager, queryClient),
  );

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => acceptInvite(authManager, invite.org),
    async onSuccess() {
      // The org list and invites are read by the route loader through
      // ensureQueryData, so no observer is mounted and a plain invalidation
      // only marks them stale: ensureQueryData would still hand the stale
      // data back to the re-run loader. refetchType "all" refetches them
      // even though they're inactive.
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: myOrgsQueryOptions(authManager).queryKey,
          refetchType: "all",
        }),
        queryClient.invalidateQueries({
          queryKey: myInvitesQueryOptions(authManager).queryKey,
          refetchType: "all",
        }),
        queryClient.invalidateQueries({ queryKey: ["opensocial"] }),
      ]);
      await router.invalidate();
    },
  });

  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          <OrgAvatar
            did={invite.org}
            name={profile?.name}
            avatarUrl={profile?.avatarUrl}
          />
          <DidHoverCard did={invite.org}>
            {profile?.name || (
              <span className="font-mono text-sm truncate">{invite.org}</span>
            )}
          </DidHoverCard>
        </div>
        {error && (
          <p className="text-sm text-destructive mt-1">
            {(error as Error).message}
          </p>
        )}
      </TableCell>
      <TableCell>
        <div className="flex gap-2 flex-wrap">
          {invite.roles.map((role) => (
            <Badge key={role} variant="outline">
              {role}
            </Badge>
          ))}
        </div>
      </TableCell>
      <TableCell className="text-right">
        <Button size="sm" disabled={isPending} onClick={() => mutate()}>
          {isPending ? "Joining…" : "Accept"}
        </Button>
      </TableCell>
    </TableRow>
  );
}
