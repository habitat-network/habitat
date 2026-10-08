import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, type AuthManager } from "internal";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "internal/components/ui";
import { ChevronsUpDownIcon } from "lucide-react";
import {
  myOrgsQueryOptions,
  orgProfileQueryOptions,
} from "@/queries/opensocial";

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
  const { data: orgs = [] } = useQuery(myOrgsQueryOptions(authManager));
  // The current org is whichever one the URL (/orgs/$org/...) names.
  const { org } = useParams({ strict: false });
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
        <DropdownMenuGroup>
          <DropdownMenuLabel>Organizations</DropdownMenuLabel>
          {orgs.map((o) => (
            <DropdownMenuItem
              key={o.did}
              onClick={() =>
                navigate({ to: "/orgs/$org", params: { org: o.did } })
              }
            >
              <OrgLabel did={o.did} authManager={authManager} />
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem render={<Link to="/orgs" />}>
          All organizations
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
