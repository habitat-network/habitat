import { listPermissions } from "@/queries/permissions";
import { createFileRoute, Link } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions/lexicons/")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component() {
    const data = Route.useLoaderData();
    return (
      <>
        <table>
          <thead>
            <tr>
              <th>Lexicon</th>
              <th>Permissions</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {Object.keys(data).map((lexicon) => (
              <tr>
                <td>{lexicon}</td>
                <td>{data[lexicon].length}</td>
                <td>
                  <Link
                    to="/permissions/lexicons/$lexiconId"
                    params={{ lexiconId: lexicon }}
                  >
                    Edit
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
