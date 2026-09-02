import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import type { AuthManager } from "internal";
import { acceptInvite, type InviteView } from "@/queries/opensocial";
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

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => acceptInvite(authManager, invite.org),
    async onSuccess() {
      await queryClient.invalidateQueries({ queryKey: ["opensocial"] });
      await router.invalidate();
    },
  });

  return (
    <TableRow>
      <TableCell>
        <DidHoverCard did={invite.org} className="font-mono text-sm truncate">
          {invite.org}
        </DidHoverCard>
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
