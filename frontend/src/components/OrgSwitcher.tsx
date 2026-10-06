import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, type AuthManager } from "internal";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "internal/components/ui";
import { ChevronsUpDownIcon } from "lucide-react";
import { orgProfileQueryOptions } from "@/queries/opensocial";
import { useSelectedOrg } from "@/lib/selectedOrg";

function OrgLabel({
  did,
  authManager,
}: {
  did: string;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(did, authManager, queryClient),
  );
  return (
    <span className="flex items-center gap-2">
      <OrgAvatar
        did={did}
        name={profile?.name}
        avatarUrl={profile?.avatarUrl}
      />
      <span className="max-w-40 truncate">{profile?.name ?? did}</span>
    </span>
  );
}

// OrgSwitcher is the app's top-level org selection: everything else (search,
// settings) is scoped to the org picked here.
export function OrgSwitcher({ authManager }: { authManager: AuthManager }) {
  const { org, orgs, select } = useSelectedOrg(authManager);
  const navigate = useNavigate();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" />}>
        {org ? (
          <OrgLabel did={org} authManager={authManager} />
        ) : (
          "Select organization"
        )}
        <ChevronsUpDownIcon />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel>Organizations</DropdownMenuLabel>
        {orgs.map((o) => (
          <DropdownMenuItem
            key={o.did}
            onClick={() => {
              select(o.did);
              // Pages under /orgs/$org are keyed by the URL, so follow it.
              if (location.pathname.startsWith("/orgs/")) {
                navigate({ to: "/orgs/$org/settings", params: { org: o.did } });
              }
            }}
          >
            <OrgLabel did={o.did} authManager={authManager} />
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem render={<Link to="/orgs" />}>
          All organizations
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
