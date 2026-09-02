import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgMembersQueryOptions, orgProfileQueryOptions } from "@/queries/opensocial";
import { DidHoverCard } from "@/components/DidHoverCard";
import { InviteMemberDialog } from "@/components/InviteMemberDialog";
import { EditProfileDialog } from "@/components/EditProfileDialog";
import { PendingOrgInvites } from "@/components/PendingOrgInvites";
import { OrgAvatar } from "internal";
import {
  Badge,
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

export const Route = createFileRoute("/_requireAuth/opensocial/$org")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgMembersQueryOptions(params.org, context.authManager, context.queryClient),
    ),
  component: OrgDetail,
});

function OrgDetail() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const members = Route.useLoaderData();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org, authManager, queryClient),
  );

  // The caller is an admin if their own membership record (already loaded
  // as part of the member list) carries the admin role.
  const isAdmin = members.some(
    (m) => m.did === authManager.getAuthInfo()?.did && m.roles.includes("admin"),
  );

  return (
    <div className="flex flex-col gap-6 py-6">
      <div>
        <Link
          to="/opensocial"
          className="text-sm text-muted-foreground hover:text-foreground"
        >
          ← All communities
        </Link>
        <div className="flex items-center justify-between gap-3 mt-2">
          <div className="flex items-center gap-3">
            <OrgAvatar
              did={org}
              name={profile?.name}
              avatarUrl={profile?.avatarUrl}
              size="lg"
            />
            <h1 className="text-2xl font-semibold">
              {profile?.name ?? <DidHoverCard did={org}>{org}</DidHoverCard>}
            </h1>
          </div>
          {isAdmin && (
            <div className="flex gap-2">
              <InviteMemberDialog org={org} authManager={authManager} />
              <EditProfileDialog
                org={org}
                name={profile?.name ?? ""}
                description={profile?.description ?? ""}
                avatarUrl={profile?.avatarUrl}
                authManager={authManager}
              />
            </div>
          )}
        </div>
        {profile?.description && (
          <p className="text-muted-foreground mt-1">{profile.description}</p>
        )}
        <p className="font-mono text-xs text-muted-foreground break-all mt-1">
          {org}
        </p>
      </div>

      {isAdmin && <PendingOrgInvites org={org} authManager={authManager} />}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Members ({members.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Member</TableHead>
                <TableHead>Roles</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.did}>
                  <TableCell>
                    <DidHoverCard did={member.did} className="font-mono">
                      {member.did}
                    </DidHoverCard>
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-2 flex-wrap">
                      {member.roles.map((role) => (
                        <Badge key={role} variant="outline">
                          {role}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {members.length === 0 && (
                <TableRow>
                  <TableCell colSpan={2} className="text-muted-foreground">
                    No members yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
