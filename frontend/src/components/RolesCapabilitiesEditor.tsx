import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import {
  putRole,
  deleteRole,
  updatePermissions,
  type ActionBinding,
  type AssignableBinding,
  type RoleView,
} from "@/queries/opensocial";
import { OPENSOCIAL_ACTIONS } from "@/lib/opensocialActions";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";

const BUILTIN_ROLES = new Set(["admin", "member"]);

// rolesForAction/rolesForAssignable read the current binding for one
// action/role out of the flat bindings/assignable arrays.
function rolesForAction(bindings: ActionBinding[], action: string): string[] {
  return bindings.find((b) => b.action === action)?.roles ?? [];
}

function rolesForAssignable(
  assignable: AssignableBinding[],
  role: string,
): string[] {
  return assignable.find((a) => a.role === role)?.roles ?? [];
}

function withAction(
  bindings: ActionBinding[],
  action: string,
  roles: string[],
): ActionBinding[] {
  const rest = bindings.filter((b) => b.action !== action);
  return roles.length > 0 ? [...rest, { action, roles }] : rest;
}

function withAssignable(
  assignable: AssignableBinding[],
  role: string,
  roles: string[],
): AssignableBinding[] {
  const rest = assignable.filter((a) => a.role !== role);
  return roles.length > 0 ? [...rest, { role, roles }] : rest;
}

export function RolesCapabilitiesEditor({
  org,
  roles,
  bindings,
  assignable,
  authManager,
}: {
  org: string;
  roles: RoleView[];
  bindings: ActionBinding[];
  assignable: AssignableBinding[];
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();

  const { mutate: save, isPending: saving } = useMutation({
    mutationFn: (next: {
      bindings: ActionBinding[];
      assignable: AssignableBinding[];
    }) => updatePermissions(authManager, org, next.bindings, next.assignable),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ["opensocial", "permissions", org],
      }),
  });

  const roleRkeys = roles.map((r) => r.rkey);

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold">Roles</h2>
          <PutRoleDialog org={org} authManager={authManager} />
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Role</TableHead>
              <TableHead>Description</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {roles.map((role) => (
              <TableRow key={role.rkey}>
                <TableCell className="font-medium">{role.name}</TableCell>
                <TableCell className="text-muted-foreground">
                  {role.description}
                </TableCell>
                <TableCell className="text-right">
                  {!BUILTIN_ROLES.has(role.rkey) && (
                    <DeleteRoleButton
                      org={org}
                      role={role}
                      authManager={authManager}
                    />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-base font-semibold">Capabilities</h2>
          <p className="text-sm text-muted-foreground">
            Which roles authorize each action. A member's authorized actions are
            the union of every role they hold.
          </p>
        </div>
        <div className="flex flex-col gap-4">
          {OPENSOCIAL_ACTIONS.map(({ action, label, hint }) => (
            <Field key={action}>
              <FieldLabel>{label}</FieldLabel>
              <p className="text-xs text-muted-foreground -mt-1">{hint}</p>
              <ToggleGroup
                value={rolesForAction(bindings, action)}
                onValueChange={(next) =>
                  save({
                    bindings: withAction(bindings, action, next),
                    assignable,
                  })
                }
                multiple
                disabled={saving}
              >
                {roleRkeys.map((rkey) => (
                  <ToggleGroupItem key={rkey} value={rkey}>
                    {roles.find((r) => r.rkey === rkey)?.name ?? rkey}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>
          ))}
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-base font-semibold">Assignable roles</h2>
          <p className="text-sm text-muted-foreground">
            Which roles a holder of each role may grant, revoke, or eject a
            holder of (via the assign-roles and eject actions above).
          </p>
        </div>
        <div className="flex flex-col gap-4">
          {roles.map((role) => (
            <Field key={role.rkey}>
              <FieldLabel>{role.name} may assign/eject</FieldLabel>
              <ToggleGroup
                value={rolesForAssignable(assignable, role.rkey)}
                onValueChange={(next) =>
                  save({
                    bindings,
                    assignable: withAssignable(assignable, role.rkey, next),
                  })
                }
                multiple
                disabled={saving}
              >
                {roleRkeys.map((rkey) => (
                  <ToggleGroupItem key={rkey} value={rkey}>
                    {roles.find((r) => r.rkey === rkey)?.name ?? rkey}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>
          ))}
        </div>
      </section>
    </div>
  );
}

function PutRoleDialog({
  org,
  authManager,
}: {
  org: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const [rkey, setRkey] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => putRole(authManager, org, rkey, name, description),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "roles", org],
      });
      setOpen(false);
      setRkey("");
      setName("");
      setDescription("");
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button size="sm" />}>New role</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New role</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (rkey.trim() && name.trim()) mutate();
          }}
        >
          <Field>
            <FieldLabel htmlFor="role-key">Key</FieldLabel>
            <Input
              id="role-key"
              value={rkey}
              onChange={(e) =>
                setRkey(
                  e.target.value.trim().toLowerCase().replace(/\s+/g, "-"),
                )
              }
              placeholder="moderator"
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="role-name">Name</FieldLabel>
            <Input
              id="role-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Moderator"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="role-description">Description</FieldLabel>
            <Textarea
              id="role-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button
              type="submit"
              disabled={isPending || !rkey.trim() || !name.trim()}
            >
              {isPending ? "Creating…" : "Create role"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeleteRoleButton({
  org,
  role,
  authManager,
}: {
  org: string;
  role: RoleView;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => deleteRole(authManager, org, role.rkey),
    async onSuccess() {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["opensocial", "roles", org],
        }),
        queryClient.invalidateQueries({
          queryKey: ["opensocial", "permissions", org],
        }),
      ]);
      setOpen(false);
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="ghost" size="sm" />}>
        Delete
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete "{role.name}"?</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          Members holding this role keep their other roles. It will be removed
          from any capability or assignable bindings that named it.
        </p>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate()}
          >
            {isPending ? "Deleting…" : "Delete role"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
