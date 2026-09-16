import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { assignRoles, type RoleView } from "@/queries/opensocial";
import {
  Button,
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldError,
  FieldLabel,
  useComboboxAnchor,
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
  const [selected, setSelected] = useState<RoleView[]>(
    roles.filter((r) => initialRoles.includes(r.rkey)),
  );
  const [searchValue, setSearchValue] = useState("");
  const anchor = useComboboxAnchor();

  const filtered = useMemo(() => {
    const q = searchValue.trim().toLowerCase();
    return roles.filter((role) => !q || role.name.toLowerCase().includes(q));
  }, [roles, searchValue]);

  const { mutate, isPending, error } = useMutation({
    mutationFn: () =>
      assignRoles(
        authManager,
        org,
        memberDid,
        selected.map((r) => r.rkey),
      ),
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
      <Field>
        <FieldLabel>Roles</FieldLabel>
        <Combobox
          items={filtered}
          onInputValueChange={setSearchValue}
          inputValue={searchValue}
          multiple
          value={selected}
          onValueChange={setSelected}
        >
          <ComboboxChips ref={anchor}>
            {selected.map((role) => (
              <ComboboxChip key={role.rkey}>{role.name}</ComboboxChip>
            ))}
            <ComboboxChipsInput placeholder="Add a role…" />
          </ComboboxChips>
          <ComboboxContent anchor={anchor}>
            <ComboboxEmpty>No roles found.</ComboboxEmpty>
            <ComboboxList>
              {(role: RoleView) => (
                <ComboboxItem key={role.rkey} value={role}>
                  {role.name}
                </ComboboxItem>
              )}
            </ComboboxList>
          </ComboboxContent>
        </Combobox>
      </Field>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <DialogFooter>
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving…" : "Save roles"}
        </Button>
      </DialogFooter>
    </form>
  );
}
