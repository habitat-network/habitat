import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  Textarea,
  toast,
} from "internal/components/ui";
import { Plus, Shield, Trash2 } from "lucide-react";
import { useState } from "react";
import { EmptyState, PageHeader } from "@/components/shell/PageHeader";
import { cn } from "@/lib/cn";
import { groupQuery } from "@/lib/queries/groups";
import { rolesQuery } from "@/lib/queries/members";
import { ACTIONS } from "@/lib/roles";
import {
  deleteRole,
  putRole,
  updatePermissions,
  type PermissionsView,
  type RolesPayload,
} from "@/server/fns/manage";

// Roles & access: the group's roles, what each may do (its permission
// bindings) and which roles it may hand out. pear stores all of it in one
// community.opensocial.permissions record, so a save replaces the whole
// thing.

export const Route = createFileRoute("/_member/g/$org/roles")({
  validateSearch: z.object({ role: z.string().optional() }),
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(groupQuery(params.org)),
      context.queryClient.ensureQueryData(rolesQuery(params.org)),
    ]),
  component: RolesPage,
});

interface Draft {
  actions: Map<string, Set<string>>;
  assignable: Map<string, Set<string>>;
}

// toDraft turns the permissions record into editable sets. A group without
// one lets admins do (and assign) everything, so that's where editing
// starts; saving then writes it down.
function toDraft(roles: RolesPayload["roles"], p: PermissionsView): Draft {
  const actions = new Map<string, Set<string>>();
  const assignable = new Map<string, Set<string>>();
  if (!p.exists) {
    for (const a of ACTIONS) actions.set(a.id, new Set(["admin"]));
    assignable.set("admin", new Set(roles.map((r) => r.id)));
  } else {
    for (const b of p.bindings) actions.set(b.action, new Set(b.roles));
    for (const a of p.assignable) assignable.set(a.role, new Set(a.roles));
  }
  return { actions, assignable };
}

function RolesPage() {
  const { org } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const queryClient = useQueryClient();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  const { data } = useSuspenseQuery(rolesQuery(org));
  const canConfigure = group.myActions.includes("community.configure");
  const roles = data.roles;
  const selectedId = search.role ?? roles[0]?.id;
  const selected = roles.find((r) => r.id === selectedId) ?? null;

  // The draft restarts from the saved record whenever a fresh one loads.
  const [edit, setEdit] = useState<{ from: RolesPayload; draft: Draft } | null>(
    null,
  );
  const draft =
    edit?.from === data ? edit.draft : toDraft(data.roles, data.permissions);
  const setDraft = (f: (d: Draft) => Draft) =>
    setEdit({ from: data, draft: f(draft) });

  const save = useMutation({
    mutationFn: (d: Draft) =>
      updatePermissions({
        data: {
          org,
          bindings: [...d.actions].map(([action, rs]) => ({
            action,
            roles: [...rs],
          })),
          assignable: [...d.assignable]
            .filter(([, rs]) => rs.size > 0)
            .map(([role, rs]) => ({ role, roles: [...rs] })),
        },
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["roles", org] }),
        queryClient.invalidateQueries({ queryKey: ["group", org] }),
      ]);
      toast.add({ type: "success", title: "Saved access" });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't save access",
        description: e.message,
      }),
  });
  const remove = useMutation({
    mutationFn: (role: string) => deleteRole({ data: { org, role } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["roles", org] });
      await navigate({ search: {} });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't delete role",
        description: e.message,
      }),
  });

  const toggle = (map: "actions" | "assignable", key: string, role: string) =>
    setDraft((d) => {
      const next = new Map(d[map]);
      const set = new Set(next.get(key) ?? []);
      if (set.has(role)) set.delete(role);
      else set.add(role);
      next.set(key, set);
      return { ...d, [map]: next };
    });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Roles & access"
        description="What each role can do in the group"
      >
        {canConfigure && <RoleDialog org={org} />}
      </PageHeader>
      <div className="flex min-h-0 flex-1">
        <nav className="flex w-64 shrink-0 flex-col gap-1 border-r p-3">
          {roles.map((r) => (
            <button
              key={r.id}
              type="button"
              onClick={() => navigate({ search: { role: r.id } })}
              className={cn(
                "flex flex-col rounded-lg px-3 py-2 text-left",
                r.id === selectedId ? "bg-muted" : "hover:bg-muted/60",
              )}
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                {r.name}
                <span className="ml-auto text-xs font-normal text-muted-foreground tabular-nums">
                  {r.memberCount}
                </span>
              </span>
              {r.description && (
                <span className="line-clamp-1 text-xs text-muted-foreground">
                  {r.description}
                </span>
              )}
            </button>
          ))}
        </nav>
        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          {!selected ? (
            <EmptyState icon={<Shield />} title="No roles yet" />
          ) : (
            <div className="mx-auto flex max-w-3xl flex-col gap-6">
              <div className="flex items-start gap-3">
                <div className="flex-1">
                  <h2 className="text-xl font-semibold">{selected.name}</h2>
                  <p className="text-sm text-muted-foreground">
                    {selected.description ?? `Role id: ${selected.id}`} ·{" "}
                    {selected.memberCount === 1
                      ? "1 member"
                      : `${selected.memberCount} members`}
                  </p>
                </div>
                {canConfigure && (
                  <>
                    <RoleDialog key={selected.id} org={org} role={selected} />
                    {selected.id !== "admin" && selected.id !== "member" && (
                      <Button
                        variant="destructive"
                        size="icon"
                        aria-label={`Delete ${selected.name}`}
                        onClick={() => remove.mutate(selected.id)}
                      >
                        <Trash2 />
                      </Button>
                    )}
                  </>
                )}
              </div>

              {!data.permissions.exists && (
                <p className="rounded-lg border bg-muted/40 p-3 text-sm text-muted-foreground">
                  This group hasn't set up permissions yet, so admins can do
                  everything and nobody else can do anything. Saving writes the
                  settings below down.
                </p>
              )}

              <Card>
                <CardHeader>
                  <CardTitle>Permissions</CardTitle>
                  <CardDescription>
                    What members with this role can do.
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col divide-y">
                  {ACTIONS.map((a) => (
                    <Toggle
                      key={a.id}
                      label={a.label}
                      description={a.description}
                      checked={
                        draft.actions.get(a.id)?.has(selected.id) ?? false
                      }
                      disabled={!canConfigure}
                      onChange={() => toggle("actions", a.id, selected.id)}
                    />
                  ))}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Can assign</CardTitle>
                  <CardDescription>
                    Roles members with {selected.name} may give others, when
                    they can assign roles.
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-wrap gap-2">
                  {roles.map((r) => {
                    const on =
                      draft.assignable.get(selected.id)?.has(r.id) ?? false;
                    return (
                      <Button
                        key={r.id}
                        size="sm"
                        variant={on ? "default" : "outline"}
                        disabled={!canConfigure}
                        onClick={() => toggle("assignable", selected.id, r.id)}
                      >
                        {r.name}
                      </Button>
                    );
                  })}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Space access</CardTitle>
                  <CardDescription>
                    Every member can read the group's members and about spaces.
                    Docs are shared from the doc itself; uploads are readable by
                    every member.
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-wrap gap-2">
                  <Badge variant="secondary">Members space · read</Badge>
                  <Badge variant="secondary">About space · read</Badge>
                  <Badge variant="secondary">Uploads · read</Badge>
                </CardContent>
              </Card>

              {canConfigure && (
                <div className="flex justify-end gap-2">
                  <Button variant="outline" onClick={() => setEdit(null)}>
                    Reset
                  </Button>
                  <Button
                    disabled={save.isPending}
                    onClick={() => save.mutate(draft)}
                  >
                    Save access
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function Toggle({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: () => void;
}) {
  return (
    <label
      className={cn(
        "flex items-center gap-3 py-3",
        !disabled && "cursor-pointer",
      )}
    >
      <div className="flex-1">
        <p className="text-sm font-medium">{label}</p>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <input
        type="checkbox"
        className="size-4 accent-primary"
        checked={checked}
        disabled={disabled}
        onChange={onChange}
      />
    </label>
  );
}

// RoleDialog creates a role, or renames one.
function RoleDialog({
  org,
  role,
}: {
  org: string;
  role?: { id: string; name: string; description: string | null };
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: Route.fullPath });
  const [open, setOpen] = useState(false);
  const [id, setId] = useState(role?.id ?? "");
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  // The form state is seeded from `role` once, but this component stays
  // mounted while the selected role changes, so re-seed it every time the
  // dialog opens. Otherwise Edit shows whichever role was selected first.
  const handleOpenChange = (next: boolean) => {
    if (next) {
      setId(role?.id ?? "");
      setName(role?.name ?? "");
      setDescription(role?.description ?? "");
    }
    setOpen(next);
  };
  const save = useMutation({
    mutationFn: () => putRole({ data: { org, role: id, name, description } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["roles", org] });
      setOpen(false);
      if (!role) await navigate({ search: { role: id } });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't save role",
        description: e.message,
      }),
  });
  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button variant={role ? "outline" : "default"} />}>
        {role ? (
          "Edit"
        ) : (
          <>
            <Plus data-icon="inline-start" />
            New role
          </>
        )}
      </DialogTrigger>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>{role ? `Edit ${role.name}` : "New role"}</DialogTitle>
            <DialogDescription>
              Give the role permissions on this page once it exists.
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="role-name">Name</FieldLabel>
            <Input
              id="role-name"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!role)
                  setId(
                    e.target.value
                      .toLowerCase()
                      .replace(/[^a-z0-9]+/g, "-")
                      .replace(/^-+|-+$/g, ""),
                  );
              }}
            />
          </Field>
          {!role && (
            <Field>
              <FieldLabel htmlFor="role-id">Id</FieldLabel>
              <Input
                id="role-id"
                value={id}
                onChange={(e) => setId(e.target.value)}
              />
              <FieldDescription>
                Lowercase letters, digits and dashes. It can't change later.
              </FieldDescription>
            </Field>
          )}
          <Field>
            <FieldLabel htmlFor="role-description">Description</FieldLabel>
            <Textarea
              id="role-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              Cancel
            </DialogClose>
            <Button type="submit" disabled={!id || save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
