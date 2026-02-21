import { listPermissions, type Permission } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { useState } from "react";

// Concrete wire types matching what the server's parseGrantees expects
interface DidGranteeObj {
  $type: "network.habitat.grantee#didGrantee";
  did: string;
}

interface PermissionInput {
  grantees: DidGranteeObj[];
  collection: string;
  rkey?: string;
}

export const Route = createFileRoute("/_requireAuth/permissions/lexicons/")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component: LexiconPermissions,
});

function LexiconPermissions() {
  const data = Route.useLoaderData() as { permissions: Permission[] };
  const { authManager } = Route.useRouteContext();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (collection: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(collection)) next.delete(collection);
      else next.add(collection);
      return next;
    });
  };

  const byCollection = (data.permissions ?? []).reduce<Record<string, Permission[]>>(
    (acc, perm) => {
      (acc[perm.collection] ??= []).push(perm);
      return acc;
    },
    {},
  );

  const addForm = useForm<{ grantee: string; collection: string; rkey: string }>(
    { defaultValues: { rkey: "" } },
  );
  const { mutate: addNew, isPending: isAddingNew } = useMutation({
    async mutationFn(formData: { grantee: string; collection: string; rkey: string }) {
      const body: PermissionInput = {
        grantees: [{ $type: "network.habitat.grantee#didGrantee", did: formData.grantee }],
        collection: formData.collection,
        ...(formData.rkey ? { rkey: formData.rkey } : {}),
      };
      await authManager?.fetch(
        `/xrpc/network.habitat.addPermission`,
        "POST",
        JSON.stringify(body),
        new Headers({ "Content-Type": "application/json" }),
      );
      addForm.reset({ rkey: "" });
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <>
      <h3>Add permission</h3>
      <form onSubmit={addForm.handleSubmit((d) => addNew(d))}>
        <label>
          Collection
          <input type="text" {...addForm.register("collection")} required />
        </label>
        <label>
          Record key (optional)
          <input type="text" {...addForm.register("rkey")} />
        </label>
        <label>
          DID
          <input type="text" {...addForm.register("grantee")} required />
        </label>
        <button type="submit" aria-busy={isAddingNew}>
          Add
        </button>
      </form>
      <table>
        <thead>
          <tr>
            <th>Collection</th>
            <th>Permissions</th>
            <th />
          </tr>
        </thead>
        {Object.entries(byCollection).map(([collection, perms]) => (
          <tbody key={collection}>
            <tr>
              <td>{collection}</td>
              <td>{perms.length}</td>
              <td>
                <button type="button" onClick={() => toggle(collection)}>
                  {expanded.has(collection) ? "Collapse" : "Expand"}
                </button>
              </td>
            </tr>
            {expanded.has(collection) && (
              <tr>
                <td colSpan={3}>
                  <CollectionDetail
                    collection={collection}
                    permissions={perms}
                    authManager={authManager}
                  />
                </td>
              </tr>
            )}
          </tbody>
        ))}
      </table>
    </>
  );
}

function CollectionDetail({
  collection,
  permissions,
  authManager,
}: {
  collection: string;
  permissions: Permission[];
  authManager: any;
}) {
  const queryClient = useQueryClient();
  const router = useRouter();

  const { mutate: remove } = useMutation({
    async mutationFn({ grantee, rkey }: { grantee: string; rkey: string }) {
      const body: PermissionInput = {
        grantees: [{ $type: "network.habitat.grantee#didGrantee", did: grantee }],
        collection,
        ...(rkey ? { rkey } : {}),
      };
      await authManager?.fetch(
        `/xrpc/network.habitat.removePermission`,
        "POST",
        JSON.stringify(body),
        new Headers({ "Content-Type": "application/json" }),
      );
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <table>
      <thead>
        <tr>
          <th>Record Key</th>
          <th>Person</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {permissions.map((perm) => (
          <tr key={`${perm.grantee}:${perm.rkey}`}>
            <td>{perm.rkey || "*"}</td>
            <td>{perm.grantee}</td>
            <td>
              <button
                type="button"
                onClick={() => remove({ grantee: perm.grantee, rkey: perm.rkey })}
              >
                Remove
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
