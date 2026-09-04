import { createFileRoute } from "@tanstack/react-router";
import {
  myOrgsQueryOptions,
  myInvitesQueryOptions,
} from "@/queries/opensocial";
import { OrgItem } from "@/components/OrgItem";
import { PendingInvites } from "@/components/PendingInvites";
import { CreateOrgDialog } from "@/components/CreateOrgDialog";
import { Card, CardContent, ItemGroup } from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/opensocial/")({
  loader: async ({ context }) => {
    const { authManager, queryClient } = context;
    const [orgs, invites] = await Promise.all([
      queryClient.ensureQueryData(myOrgsQueryOptions(authManager)),
      queryClient.ensureQueryData(myInvitesQueryOptions(authManager)),
    ]);
    return { orgs, invites };
  },
  component: OpensocialIndex,
});

function OpensocialIndex() {
  const { authManager } = Route.useRouteContext();
  const { orgs, invites } = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-6 py-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Organizations</h1>
          <p className="text-muted-foreground text-sm">
            Organizations you belong to.
          </p>
        </div>
        <CreateOrgDialog authManager={authManager} />
      </div>

      {invites.length > 0 && (
        <PendingInvites invites={invites} authManager={authManager} />
      )}

      {orgs.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            You aren&rsquo;t a member of any organizations yet. Create one to
            get started.
          </CardContent>
        </Card>
      ) : (
        <ItemGroup className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {orgs.map((org) => (
            <OrgItem key={org.did} org={org} authManager={authManager} />
          ))}
        </ItemGroup>
      )}
    </div>
  );
}
