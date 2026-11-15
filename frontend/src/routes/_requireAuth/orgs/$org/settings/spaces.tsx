import { createFileRoute, Link } from "@tanstack/react-router";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { SpaceRef } from "@atproto/syntax";
import {
  orgMembersQueryOptions,
  orgRolesQueryOptions,
  spaceAccessQueryOptions,
} from "@/queries/opensocial";
import { spacesListQueryOptions } from "@/queries/spaces";
import { profilesQueryOptions } from "@/queries/profiles";
import { explainSpaceReaders } from "@/lib/spaceAccess";
import { DidHoverCard } from "@/components/DidHoverCard";
import { UserAvatar, UserDisplayName, type Actor } from "internal";
import {
  Badge,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/spaces")(
  {
    loader: ({ context, params }) =>
      context.queryClient.ensureQueryData(
        spacesListQueryOptions(context.authManager, { did: params.org }),
      ),
    component: OrgSpaces,
  },
);

// The community's own spaces are readable by every member (and any app), so
// they carry no access record to explain.
const COMMUNITY_SPACE_TYPES = new Set([
  "community.opensocial.members",
  "community.opensocial.about",
]);

function OrgSpaces() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: spaces = [] } = useQuery(
    spacesListQueryOptions(authManager, { did: org }),
  );
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: roles = [] } = useQuery(
    orgRolesQueryOptions(org, authManager, queryClient),
  );
  const { data: profiles } = useQuery(
    profilesQueryOptions(
      org,
      members.map((m) => m.did),
      authManager,
      queryClient,
    ),
  );
  const profileByDid = new Map<string, Actor>(profiles?.map((p) => [p.did, p]));
  const roleName = (rkey: string) =>
    roles.find((r) => r.rkey === rkey)?.name ?? rkey;

  const access = useQueries({
    queries: spaces.map((space) => ({
      ...spaceAccessQueryOptions(org, space.uri, authManager, queryClient),
      enabled: !COMMUNITY_SPACE_TYPES.has(SpaceRef.parse(space.uri).spaceType),
    })),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold">Spaces ({spaces.length})</h2>
        <p className="text-sm text-muted-foreground">
          Each space lists the roles that may read it. A member can read and
          write a space when they hold at least one of those roles. This page
          shows the spaces you can see yourself.
        </p>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Space</TableHead>
            <TableHead>Roles with access</TableHead>
            <TableHead>Who can read it</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {spaces.map((space, i) => {
            const ref = SpaceRef.parse(space.uri);
            const everyone = COMMUNITY_SPACE_TYPES.has(ref.spaceType);
            const { data: accessRoles, isLoading, error } = access[i];
            const readers = everyone
              ? members.map((m) => ({ did: m.did, via: [] as string[] }))
              : explainSpaceReaders(accessRoles, members);
            return (
              <TableRow key={space.uri} className="align-top">
                <TableCell>
                  <Link
                    to="/spaces/$spaceOwner/$spaceType/$spaceKey"
                    params={{
                      spaceOwner: ref.spaceDid,
                      spaceType: ref.spaceType,
                      spaceKey: ref.skey,
                    }}
                    className="hover:underline"
                  >
                    <span className="font-mono text-xs break-all">
                      {ref.spaceType}
                    </span>
                    <span className="block font-mono text-xs text-muted-foreground break-all">
                      {ref.skey}
                    </span>
                  </Link>
                </TableCell>
                <TableCell>
                  {everyone ? (
                    <span className="text-sm text-muted-foreground">
                      Every member
                    </span>
                  ) : isLoading ? (
                    <span className="text-sm text-muted-foreground">
                      Loading…
                    </span>
                  ) : error ? (
                    <span className="text-sm text-destructive">
                      Couldn't read the access record
                    </span>
                  ) : !accessRoles || accessRoles.length === 0 ? (
                    <span className="text-sm text-muted-foreground">
                      No roles. Nobody can read this space.
                    </span>
                  ) : (
                    <div className="flex gap-2 flex-wrap">
                      {accessRoles.map((role) => (
                        <Badge key={role} variant="outline">
                          {roleName(role)}
                        </Badge>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <Collapsible>
                    <CollapsibleTrigger className="text-sm hover:underline">
                      {readers.length}{" "}
                      {readers.length === 1 ? "member" : "members"}
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <ul className="flex flex-col gap-2 pt-2">
                        {readers.map((reader) => {
                          const actor = profileByDid.get(reader.did) ?? {
                            did: reader.did,
                          };
                          return (
                            <li
                              key={reader.did}
                              className="flex items-center gap-2 flex-wrap"
                            >
                              <DidHoverCard did={reader.did} org={org}>
                                <div className="flex items-center gap-2">
                                  <UserAvatar actor={actor} size="sm" />
                                  <UserDisplayName actor={actor} />
                                </div>
                              </DidHoverCard>
                              {reader.via.length > 0 && (
                                <span className="text-xs text-muted-foreground">
                                  via {reader.via.map(roleName).join(", ")}
                                </span>
                              )}
                            </li>
                          );
                        })}
                      </ul>
                    </CollapsibleContent>
                  </Collapsible>
                </TableCell>
              </TableRow>
            );
          })}
          {spaces.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="text-muted-foreground">
                No spaces yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
