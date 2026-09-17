import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgMembersQueryOptions,
  orgRolesQueryOptions,
} from "@/queries/opensocial";
import { profilesQueryOptions } from "@/queries/profiles";
import { DidHoverCard } from "@/components/DidHoverCard";
import { InviteMemberDialog } from "@/components/InviteMemberDialog";
import { PendingOrgInvites } from "@/components/PendingOrgInvites";
import { AssignRolesDialog } from "@/components/AssignRolesDialog";
import { EjectMemberButton } from "@/components/EjectMemberButton";
import { UserAvatar, UserDisplayName, type Actor } from "internal";
import {
  Badge,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/members")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgMembersQueryOptions(
        params.org,
        context.authManager,
        context.queryClient,
      ),
    ),
  component: OrgMembers,
});

function OrgMembers() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: roles = [] } = useQuery(
    orgRolesQueryOptions(org, authManager, queryClient),
  );
  const { data: profiles } = useQuery(
    profilesQueryOptions(
      members.map((m) => m.did),
      queryClient,
    ),
  );
  const profileByDid = new Map<string, Actor>(profiles?.map((p) => [p.did, p]));

  const isAdmin = members.some(
    (m) =>
      m.did === authManager.getAuthInfo()?.did && m.roles.includes("admin"),
  );

  return (
    <div className="flex flex-col gap-6">
      {isAdmin && <PendingOrgInvites org={org} authManager={authManager} />}

      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">Members ({members.length})</h2>
        {isAdmin && <InviteMemberDialog org={org} authManager={authManager} />}
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Member</TableHead>
            <TableHead>Roles</TableHead>
            {isAdmin && <TableHead />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((member) => (
            <TableRow key={member.did}>
              <TableCell>
                <DidHoverCard did={member.did}>
                  <div className="flex items-center gap-2">
                    <UserAvatar
                      actor={
                        profileByDid.get(member.did) ?? { did: member.did }
                      }
                      size="sm"
                    />
                    <UserDisplayName
                      actor={
                        profileByDid.get(member.did) ?? { did: member.did }
                      }
                    />
                  </div>
                </DidHoverCard>
              </TableCell>
              <TableCell>
                <div className="flex gap-2 flex-wrap">
                  {member.roles.map((role) => (
                    <Badge key={role} variant="outline">
                      {roles.find((r) => r.rkey === role)?.name ?? role}
                    </Badge>
                  ))}
                </div>
              </TableCell>
              {isAdmin && (
                <TableCell className="text-right">
                  <div className="flex gap-2 justify-end">
                    <AssignRolesDialog
                      org={org}
                      memberDid={member.did}
                      currentRoles={member.roles}
                      roles={roles}
                      authManager={authManager}
                    />
                    <EjectMemberButton
                      org={org}
                      memberDid={member.did}
                      authManager={authManager}
                    />
                  </div>
                </TableCell>
              )}
            </TableRow>
          ))}
          {members.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="text-muted-foreground">
                No members yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
