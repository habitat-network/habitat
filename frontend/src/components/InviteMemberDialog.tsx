import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { UserCombobox, type Actor, type AuthManager } from "internal";
import { createInvite } from "@/queries/opensocial";
import {
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldError,
  FieldLabel,
} from "internal/components/ui";

export function InviteMemberDialog({
  org,
  authManager,
}: {
  org: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const [invitees, setInvitees] = useState<Actor[]>([]);
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () =>
      Promise.all(
        invitees.map((actor) => createInvite(authManager, org, actor.did)),
      ),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "pendingInvites", org],
      });
      setOpen(false);
      setInvitees([]);
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setInvitees([]);
      }}
    >
      <DialogTrigger render={<Button>Invite</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Invite a member</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (invitees.length > 0) mutate();
          }}
        >
          <Field>
            <FieldLabel>People</FieldLabel>
            <UserCombobox
              value={invitees}
              onValueChange={setInvitees}
              identityResolverUrl={
                "https://" + import.meta.env.VITE_HABITAT_DOMAIN
              }
            />
            <FieldError errors={error ? [{ message: error.message }] : []} />
          </Field>
          <DialogFooter>
            <Button type="submit" disabled={isPending || invitees.length === 0}>
              {isPending ? "Inviting…" : "Send invite"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
