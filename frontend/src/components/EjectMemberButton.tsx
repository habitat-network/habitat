import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { ejectMember } from "@/queries/opensocial";
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

export function EjectMemberButton({
  org,
  memberDid,
  authManager,
}: {
  org: string;
  memberDid: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => ejectMember(authManager, org, memberDid),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "members", org],
      });
      setOpen(false);
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        Remove
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove member</DialogTitle>
          <DialogDescription>
            This revokes every role this member holds, along with any access
            those roles granted. They can be invited back later.
          </DialogDescription>
        </DialogHeader>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate()}
          >
            {isPending ? "Removing…" : "Remove member"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
