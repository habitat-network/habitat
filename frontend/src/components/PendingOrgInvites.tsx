import { useQuery, useQueryClient } from "@tanstack/react-query";
import { UserAvatar, UserDisplayName, type Actor } from "internal";
import type { AuthManager } from "internal";
import { orgPendingInvitesQueryOptions } from "@/queries/opensocial";
import { profilesQueryOptions } from "@/queries/profiles";
import { DidHoverCard } from "@/components/DidHoverCard";
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

// PendingOrgInvites lists a community's outstanding invites for its admins.
export function PendingOrgInvites({
  org,
  authManager,
}: {
  org: string;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const { data: invites = [] } = useQuery(
    orgPendingInvitesQueryOptions(org, authManager),
  );
  const { data: profiles } = useQuery(
    profilesQueryOptions(
      invites.map((invite) => invite.invitee),
      queryClient,
    ),
  );
  const profileByDid = new Map<string, Actor>(profiles?.map((p) => [p.did, p]));

  if (invites.length === 0) return null;

  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="text-base">
          Pending invites ({invites.length})
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Invitee</TableHead>
              <TableHead>Roles</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {invites.map((invite) => (
              <TableRow key={invite.id}>
                <TableCell>
                  <DidHoverCard did={invite.invitee}>
                    <div className="flex items-center gap-2">
                      <UserAvatar
                        actor={
                          profileByDid.get(invite.invitee) ?? {
                            did: invite.invitee,
                          }
                        }
                        size="sm"
                      />
                      <UserDisplayName
                        actor={
                          profileByDid.get(invite.invitee) ?? {
                            did: invite.invitee,
                          }
                        }
                      />
                    </div>
                  </DidHoverCard>
                </TableCell>
                <TableCell>
                  <div className="flex gap-2 flex-wrap">
                    {invite.roles.map((role) => (
                      <Badge key={role} variant="outline">
                        {role}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
