import { createFileRoute, Link } from "@tanstack/react-router";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  clientMetadataQueryOptions,
  orgAppAccessQueryOptions,
} from "@/queries/opensocial";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/apps")({
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
  // Names and logos come from each app's client metadata document; an app
  // whose document can't be fetched is still listed by its client ID.
  const metadata = useQueries({
    queries: appAccess.map((app) => clientMetadataQueryOptions(app.clientId)),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold">
          Authorized apps ({appAccess.length})
        </h2>
        <p className="text-sm text-muted-foreground">
          Apps an admin has authorized for this organization. An authorized app
          can read the organization's spaces on behalf of the members who sign
          in to it.
        </p>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>App</TableHead>
            <TableHead>Client ID</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {appAccess.map((app, i) => {
            const client = metadata[i]?.data?.metadata;
            return (
              <TableRow key={app.clientId}>
                <TableCell>
                  <Link
                    to="/orgs/$org/settings/app/$clientKey"
                    params={{ org, clientKey: app.rkey }}
                    className="flex items-center gap-2 hover:underline"
                  >
                    {client?.logo_uri && (
                      <img
                        src={client.logo_uri}
                        alt=""
                        className="size-6 rounded border"
                      />
                    )}
                    <span className="font-medium">
                      {client?.client_name ?? "Unnamed app"}
                    </span>
                  </Link>
                </TableCell>
                <TableCell className="font-mono text-xs break-all text-muted-foreground">
                  {app.clientId}
                </TableCell>
              </TableRow>
            );
          })}
          {appAccess.length === 0 && (
            <TableRow>
              <TableCell colSpan={2} className="text-muted-foreground">
                No authorized apps yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
