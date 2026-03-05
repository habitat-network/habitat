import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const did = context.authManager.getAuthInfo()?.did;
    const response = await context.authManager.fetch(
      `/xrpc/network.habitat.listRecords?subjects=${did}&collection=network.habitat.docs`,
    );
    const data: {
      records: {
        uri: string;
        cid: string;
        value: HabitatDoc;
      }[];
    } = await response?.json();
    const profile = await context.authManager.client().getSelfProfile()

    return { profile, data };
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const router = useRouter();
    const { profile, data } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const navigate = Route.useNavigate();

    const { mutate: create, isPending } = useMutation({
      mutationFn: async () => {
        const did = authManager.getAuthInfo()?.did;
        const response = await authManager.fetch(
          `/xrpc/network.habitat.putRecord`,
          "POST",
          JSON.stringify({
            repo: did,
            collection: "network.habitat.docs",
            record: {
              name: "Untitled",
              blob: null,
            },
          }),
        );

        const { uri } = await response.json();
        navigate({
          to: "/$uri",
          params: {
            uri: uri,
          },
        });
        router.invalidate({ filter: (x) => x.pathname === "/docs/" });
      },
    });


    return (
      <>
        <p>Logged in as: @{profile.handle}</p>
        <button type="submit" aria-busy={isPending} onClick={() => create()}>
          New
        </button>
        <table>
          <thead>
            <tr>
              <th>Name</th>
            </tr>
          </thead>
          <tbody>
            {data.records.map((doc) => (
              <tr key={doc.cid}>
                <td>
                  <Link to="/$uri" params={{ uri: doc.uri }}>
                    {doc.value.name || doc.uri}
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </>
    );
  },
});
