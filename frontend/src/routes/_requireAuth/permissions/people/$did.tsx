import { listPermissions } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { procedure } from "internal";

export const Route = createFileRoute("/_requireAuth/permissions/people/$did")({
  async loader({ context, params }) {
    const data = await context.queryClient.fetchQuery(
      listPermissions(context.authManager),
    );
    return data.permissions
      .filter((p) => p.grantee === params.did)
      .map((p) => ({ collection: p.collection, rkey: p.rkey }))
      .sort((a, b) => a.collection.localeCompare(b.collection));
  },
  component: PersonDetail,
});

function PersonDetail() {
  const permissions = Route.useLoaderData();
  const { did } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();

  const { mutate: remove } = useMutation({
    async mutationFn({
      collection,
      rkey,
    }: {
      collection: string;
      rkey?: string;
    }) {
      await procedure(
        "network.habitat.permissions.removePermission",
        {
          grantees: [{ $type: "network.habitat.grantee#didGrantee", did }],
          collection,
          ...(rkey ? { rkey } : {}),
        },
        { fetcher: authManager },
      );
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <>
      <h3>{did}</h3>
      <table>
        <thead>
          <tr>
            <th>Collection</th>
            <th>Record Key</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {permissions.length === 0 && (
            <tr>
              <td colSpan={3}>No permissions for this person.</td>
            </tr>
          )}
          {permissions.map((perm) => (
            <tr key={`${perm.collection}:${perm.rkey}`}>
              <td>{perm.collection}</td>
              <td>{perm.rkey || "*"}</td>
              <td>
                <button type="button" onClick={() => remove(perm)}>
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
