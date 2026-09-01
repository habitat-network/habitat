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
      <CardContent className="flex flex-col gap-3">
        {invites.map((invite) => (
          <PendingInviteRow
            key={`${invite.org}:${invite.id}`}
            invite={invite}
            authManager={authManager}
          />
        ))}
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
    <div className="flex flex-col gap-2 border rounded-md p-3">
      <div className="flex items-center justify-between gap-2">
        <DidHoverCard did={invite.org} className="font-mono text-sm truncate">
          {invite.org}
        </DidHoverCard>
        <Button size="sm" disabled={isPending} onClick={() => mutate()}>
          {isPending ? "Joining…" : "Accept"}
        </Button>
      </div>
      <div className="flex gap-2 flex-wrap">
        {invite.roles.map((role) => (
          <Badge key={role} variant="outline">
            {role}
          </Badge>
        ))}
      </div>
      {error && (
        <p className="text-sm text-destructive">{(error as Error).message}</p>
      )}
    </div>
  );
}
