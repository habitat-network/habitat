import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { OrgAvatar } from "internal";
import { orgProfileQueryOptions, type OrgSummary } from "@/queries/opensocial";
import { DidHoverCard } from "@/components/DidHoverCard";
import {
  Item,
  ItemMedia,
  ItemContent,
  ItemTitle,
  ItemDescription,
} from "internal/components/ui";

export function OrgItem({
  org,
  authManager,
}: {
  org: OrgSummary;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org.did, authManager, queryClient),
  );

  return (
    <Item
      variant="outline"
      render={<Link to="/opensocial/$org" params={{ org: org.did }} />}
    >
      <ItemMedia>
        <OrgAvatar
          did={org.did}
          name={profile?.name}
          avatarUrl={profile?.avatarUrl}
        />
      </ItemMedia>
      <ItemContent>
        <ItemTitle>
          {profile?.name ?? (
            <DidHoverCard did={org.did}>{org.did}</DidHoverCard>
          )}
        </ItemTitle>
        {profile?.description && (
          <ItemDescription>{profile.description}</ItemDescription>
        )}
        <span className="font-mono text-xs text-muted-foreground break-all">
          {org.did}
        </span>
      </ItemContent>
    </Item>
  );
}
