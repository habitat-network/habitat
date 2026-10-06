import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgMembersQueryOptions,
  orgPermissionsQueryOptions,
} from "@/queries/opensocial";
import { orgSearchCollectionsQueryOptions } from "@/queries/searchConfig";
import { SearchCollectionsEditor } from "@/components/SearchCollectionsEditor";
import {
  ACTION_COMMUNITY_CONFIGURE,
  hasOpensocialAction,
} from "@/lib/opensocialActions";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/search")(
  {
    loader: ({ context, params }) =>
      context.queryClient.ensureQueryData(
        orgPermissionsQueryOptions(
          params.org,
          context.authManager,
          context.queryClient,
        ),
      ),
    component: OrgSearch,
  },
);

function OrgSearch() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: permissions } = useQuery(
    orgPermissionsQueryOptions(org, authManager, queryClient),
  );
  const userRoles =
    members.find((m) => m.did === authManager.getAuthInfo()?.did)?.roles ?? [];
  const canConfigure = hasOpensocialAction(
    permissions?.bindings ?? [],
    userRoles,
    ACTION_COMMUNITY_CONFIGURE,
  );
  // Listing the configuration requires the same action as changing it, so
  // other members don't query it.
  const { data } = useQuery(
    orgSearchCollectionsQueryOptions(org, authManager, canConfigure),
  );

  if (!canConfigure) {
    return (
      <p className="text-sm text-muted-foreground">
        Only admins can configure which collections show up in search.
      </p>
    );
  }
  return (
    <SearchCollectionsEditor
      org={org}
      collections={data?.collections ?? []}
      defaults={data?.defaults ?? []}
      canConfigure={canConfigure}
      authManager={authManager}
    />
  );
}
