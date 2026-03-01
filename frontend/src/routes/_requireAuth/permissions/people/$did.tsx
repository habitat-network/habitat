import { listPermissions } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions/people/$did")({
  async loader({ context, params }) {
    const data = await context.queryClient.fetchQuery(
      listPermissions(context.authManager),
    );
    // Derive lexicons for this person from the already-cached full map
    const lexicons = data.permissions
      .filter((p) => p.grantee === params.did)
      .map((p) => p.collection + "." + p.rkey);
    return lexicons.sort();
  },
  component: PersonDetail,
});

function PersonDetail() {
  const lexicons = Route.useLoaderData();
  const { did } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();

  const { mutate: remove } = useMutation({
    async mutationFn(lexicon: string) {
      await authManager?.fetch(
        `/xrpc/network.habitat.removePermission`,
        "POST",
        JSON.stringify({ did, lexicon }),
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
            <th>NSID</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {lexicons.length === 0 && (
            <tr>
              <td colSpan={2}>No permissions for this person.</td>
            </tr>
          )}
          {lexicons.map((lexicon) => (
            <tr key={lexicon}>
              <td>{lexicon}</td>
              <td>
                <button type="button" onClick={() => remove(lexicon)}>
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
