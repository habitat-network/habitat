import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { DidString } from "@atproto/lex";
import type { AuthManager } from "internal";
import { revokeAppAccess } from "@/queries/opensocial";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  FieldError,
} from "internal/components/ui";

export function RevokeAppButton({
  org,
  clientId,
  appName,
  authManager,
}: {
  org: DidString;
  clientId: string;
  appName: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => revokeAppAccess(authManager, org, clientId),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "appAccess", org],
      });
      setOpen(false);
      await navigate({ to: "/orgs/$org/settings/apps", params: { org } });
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="destructive" size="sm" />}>
        Revoke access
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Revoke "{appName}"?</DialogTitle>
          <DialogDescription>
            The app will no longer be able to read this organization's spaces or
            act as the organization. Data it already copied stays with the app.
            An admin can authorize it again by signing in to the app.
          </DialogDescription>
        </DialogHeader>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate()}
          >
            {isPending ? "Revoking…" : "Revoke access"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
