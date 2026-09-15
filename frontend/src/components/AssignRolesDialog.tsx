import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { assignRoles, type RoleView } from "@/queries/opensocial";
import {
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  FieldError,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";

export function AssignRolesDialog({
  org,
  memberDid,
  currentRoles,
  roles,
  authManager,
}: {
  org: string;
  memberDid: string;
  currentRoles: string[];
  roles: RoleView[];
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        Edit roles
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit roles</DialogTitle>
        </DialogHeader>
        {open && (
          <AssignRolesForm
            org={org}
            memberDid={memberDid}
            initialRoles={currentRoles}
            roles={roles}
            authManager={authManager}
            onSaved={() => setOpen(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function AssignRolesForm({
  org,
  memberDid,
  initialRoles,
  roles,
  authManager,
  onSaved,
}: {
  org: string;
  memberDid: string;
  initialRoles: string[];
  roles: RoleView[];
  authManager: AuthManager;
  onSaved: () => void;
}) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<string[]>(initialRoles);

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => assignRoles(authManager, org, memberDid, selected),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "members", org],
      });
      onSaved();
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        mutate();
      }}
    >
      <ToggleGroup
        value={selected}
        onValueChange={setSelected}
        multiple
        orientation="vertical"
        className="items-stretch"
      >
        {roles.map((role) => (
          <ToggleGroupItem key={role.rkey} value={role.rkey}>
            {role.name}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <DialogFooter>
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving…" : "Save roles"}
        </Button>
      </DialogFooter>
    </form>
  );
}
