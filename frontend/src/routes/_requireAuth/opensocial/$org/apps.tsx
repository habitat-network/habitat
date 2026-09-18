import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgAppAccessQueryOptions } from "@/queries/opensocial";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/apps")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgAppAccessQueryOptions(
        params.org,
        context.authManager,
        context.queryClient,
      ),
    ),
  component: OrgApps,
});

function OrgApps() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: appAccess = [] } = useQuery(
    orgAppAccessQueryOptions(org, authManager, queryClient),
  );

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-base font-semibold">
        Authorized apps ({appAccess.length})
      </h2>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Client ID</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {appAccess.map((app) => (
            <TableRow key={app.clientId}>
              <TableCell className="font-mono text-xs break-all">
                <Link
                  to="/opensocial/$org/app/$clientKey"
                  params={{ org, clientKey: app.rkey }}
                  className="hover:underline"
                >
                  {app.clientId}
                </Link>
              </TableCell>
            </TableRow>
          ))}
          {appAccess.length === 0 && (
            <TableRow>
              <TableCell className="text-muted-foreground">
                No authorized apps yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
