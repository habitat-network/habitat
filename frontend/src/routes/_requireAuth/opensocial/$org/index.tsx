import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgAppAccessQueryOptions,
  orgMembersQueryOptions,
  orgRolesQueryOptions,
} from "@/queries/opensocial";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/")({
  component: OrgOverview,
});

function OrgOverview() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: roles = [] } = useQuery(
    orgRolesQueryOptions(org, authManager, queryClient),
  );
  const { data: appAccess = [] } = useQuery(
    orgAppAccessQueryOptions(org, authManager, queryClient),
  );

  const stats = [
    { label: "Members", value: members.length },
    { label: "Roles", value: roles.length },
    { label: "Authorized apps", value: appAccess.length },
  ];

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
      {stats.map((stat) => (
        <Card key={stat.label} size="sm">
          <CardHeader>
            <CardTitle className="text-sm text-muted-foreground font-normal">
              {stat.label}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold">{stat.value}</p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
