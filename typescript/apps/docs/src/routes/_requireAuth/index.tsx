import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const did = context.authManager.handle;
    const response = await context.authManager.fetch(
      `/xrpc/network.habitat.listRecords?repo=${did}&collection=com.habitat.docs`,
    );
    const data: {
      records: {
        uri: string;
        cid: string;
        value: HabitatDoc;
      }[];
    } = await response?.json();
    return data;
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const router = useRouter();
    const { records } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const navigate = Route.useNavigate();

    const { mutate: create, isPending } = useMutation({
      mutationFn: async () => {
        const did = authManager.handle;
        const response = await authManager.fetch(
          `/xrpc/network.habitat.putRecord`,
          "POST",
          JSON.stringify({
            repo: did,
            collection: "com.habitat.docs",
            record: {
              name: "Untitled",
              blob: null,
            },
          }),
        );
        if (!response?.ok) {
          throw new Error("Failed to create doc");
        }
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
            {records.map((doc) => (
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
