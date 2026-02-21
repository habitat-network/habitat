import { listPermissions, type Permission } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

// Concrete wire types for the grantee union variants the server parses
interface DidGranteeObj {
  $type: "network.habitat.grantee#didGrantee";
  did: string;
}

// Matches InputSchema from typescript/api/types/network/habitat/permissions/{add,remove}Permission.ts
interface PermissionInput {
  grantees: DidGranteeObj[];
  collection: string;
  rkey?: string;
}

interface FormData {
  grantee: string;
  collection: string;
  rkey: string;
}

export const Route = createFileRoute(
  "/_requireAuth/permissions/lexicons/$collection",
)({
  async loader({ context, params }) {
    const response = await context.queryClient.fetchQuery(
      listPermissions(context.authManager),
    );
    return (response.permissions ?? []).filter(
      (p) => p.collection === params.collection,
    );
  },
  component() {
    const router = useRouter();
    const queryClient = useQueryClient();
    const { authManager } = Route.useRouteContext();
    const params = Route.useParams();
    const permissions = Route.useLoaderData() as Permission[];
    const form = useForm<FormData>({
      defaultValues: { collection: params.collection, rkey: "" },
    });

    const { mutate: add, isPending: isAdding } = useMutation({
      async mutationFn(data: FormData) {
        const body: PermissionInput = {
          grantees: [{ $type: "network.habitat.grantee#didGrantee", did: data.grantee }],
          collection: data.collection,
          ...(data.rkey ? { rkey: data.rkey } : {}),
        };
        await authManager?.fetch(
          `/xrpc/network.habitat.addPermission`,
          "POST",
          JSON.stringify(body),
          new Headers({ "Content-Type": "application/json" }),
        );
        form.reset({ collection: params.collection, rkey: "" });
        await queryClient.invalidateQueries({ queryKey: ["permissions"] });
        router.invalidate();
      },
      onError(e) {
        console.error(e);
      },
    });

    const { mutate: remove } = useMutation({
      async mutationFn({ grantee, rkey }: { grantee: string; rkey: string }) {
        const body: PermissionInput = {
          grantees: [{ $type: "network.habitat.grantee#didGrantee", did: grantee }],
          collection: params.collection,
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
      onError(e) {
        console.error(e);
      },
    });

    return (
      <>
        <h3>{params.collection}</h3>
        <form onSubmit={form.handleSubmit((data) => add(data))}>
          <fieldset>
            <input
              type="text"
              placeholder="DID (did:plc:...)"
              {...form.register("grantee")}
            />
            <input
              type="text"
              placeholder="Collection NSID"
              {...form.register("collection")}
            />
            <input
              type="text"
              placeholder="Record key (optional)"
              {...form.register("rkey")}
            />
            <button type="submit" aria-busy={isAdding}>
              Add
            </button>
          </fieldset>
        </form>
        <table>
          <thead>
            <tr>
              <th>Record Key</th>
              <th>Person</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {permissions?.map((perm) => (
              <tr key={`${perm.grantee}:${perm.rkey}`}>
                <td>{perm.rkey || "*"}</td>
                <td>{perm.grantee}</td>
                <td>
                  <button
                    type="button"
                    onClick={() => remove({ grantee: perm.grantee, rkey: perm.rkey })}
                  >
                    🗑️
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </>
    );
  },
});
