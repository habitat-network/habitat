import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgAppAccessQueryOptions } from "@/queries/opensocial";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
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
    <Card size="sm">
      <CardHeader>
        <CardTitle className="text-base">
          Authorized apps ({appAccess.length})
        </CardTitle>
      </CardHeader>
      <CardContent>
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
      </CardContent>
    </Card>
  );
}
