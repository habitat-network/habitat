import { listPermissions } from "@/queries/permissions";
import { createFileRoute, Link, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions/people")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component: PeoplePermissions,
});

/** Invert the lexicon->dids map into a did->lexicons map */
function invertPermissions(
  data: Record<string, string[]>,
): Record<string, string[]> {
  const byPerson: Record<string, string[]> = {};
  for (const [lexicon, dids] of Object.entries(data)) {
    for (const did of dids) {
      if (!byPerson[did]) byPerson[did] = [];
      byPerson[did].push(lexicon);
    }
  }
  return byPerson;
}

function PeoplePermissions() {
  const data = Route.useLoaderData() as Record<string, string[]>;
  const byPerson = invertPermissions(data);
  const people = Object.keys(byPerson).sort();

  return (
    <>
      <table>
        <thead>
          <tr>
            <th>Person (DID)</th>
            <th>Permissions</th>
          </tr>
        </thead>
        <tbody>
          {people.length === 0 && (
            <tr>
              <td colSpan={2}>No permissions found.</td>
            </tr>
          )}
          {people.map((did) => (
            <tr key={did}>
              <td>
                <Link
                  to="/permissions/people/$did"
                  params={{ did }}
                >
                  {did}
                </Link>
              </td>
              <td>{byPerson[did].length}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <Outlet />
    </>
  );
}
