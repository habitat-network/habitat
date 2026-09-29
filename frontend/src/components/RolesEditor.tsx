import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { putRole, deleteRole, type RoleView } from "@/queries/opensocial";
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
} from "internal/components/ui";

const BUILTIN_ROLES = new Set(["admin", "member"]);

export function RolesEditor({
  org,
  roles,
  authManager,
}: {
  org: string;
  roles: RoleView[];
  authManager: AuthManager;
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">Roles ({roles.length})</h2>
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
          {roles.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="text-muted-foreground">
                No roles yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
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
