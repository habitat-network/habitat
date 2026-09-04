import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import type { AuthManager } from "internal";
import { createOrg, acceptInvite } from "@/queries/opensocial";
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
  Input,
} from "internal/components/ui";

export function CreateOrgDialog({ authManager }: { authManager: AuthManager }) {
  const queryClient = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [handle, setHandle] = useState("");

  const { mutate, isPending, error } = useMutation({
    mutationFn: async () => {
      const { org } = await createOrg(authManager, handle);
      // NewOrg grants the creator the admin role but, since that happens
      // under the org's own authority, doesn't write anything under the
      // creator's own repo. Confirm membership under the creator's own
      // credentials so they show up in their own "my communities" listing,
      // the same way an invited member does after requestJoin.
      await acceptInvite(authManager, org);
      return org;
    },
    async onSuccess(org) {
      await queryClient.invalidateQueries({ queryKey: ["opensocial"] });
      setOpen(false);
      setHandle("");
      await navigate({ to: "/opensocial/$org", params: { org } });
      // Router loader results are cached independent of the query client
      // (see __root's hour-long staleTime), so invalidating before
      // navigating away doesn't reach the /opensocial route's own loader
      // cache. Invalidate again once it's no longer the active route so a
      // later visit re-runs its loader instead of serving stale org list.
      await router.invalidate();
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setHandle("");
      }}
    >
      <DialogTrigger render={<Button>New community</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create a community</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (handle.trim()) mutate();
          }}
        >
          <Field>
            <FieldLabel htmlFor="org-handle">Handle</FieldLabel>
            <Input
              id="org-handle"
              value={handle}
              onChange={(e) => setHandle(e.target.value)}
              placeholder="acmecorp"
              autoFocus
            />
            <FieldError errors={error ? [{ message: error.message }] : []} />
          </Field>
          <DialogFooter>
            <Button type="submit" disabled={isPending || !handle.trim()}>
              {isPending ? "Creating…" : "Create organization"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
