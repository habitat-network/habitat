import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { UserAvatar } from "internal";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
  Spinner,
} from "internal/components/ui";
import { profileQueryOptions } from "@/queries/profiles";

export interface DidHoverCardProps {
  did: string;
  // org whose members' profiles to look the DID up in. Habitat's getProfiles
  // is scoped to an org, so without one there is no profile to show.
  org?: string;
  children: ReactNode;
  className?: string;
}

// DidHoverCard shows the org member profile behind a DID on hover. The spaces
// pages render raw DIDs everywhere — as owners, repos and members — and a DID
// on its own says nothing about who it is.
export function DidHoverCard({
  did,
  org,
  children,
  className,
}: DidHoverCardProps) {
  const [open, setOpen] = useState(false);
  const { authManager } = useRouteContext({ from: "__root__" });

  // getProfiles only answers for DIDs, and the profile is only worth fetching
  // once the card has actually been opened.
  const isDid = did.startsWith("did:");
  const { data, isError } = useQuery({
    ...profileQueryOptions(org ?? "", did, authManager),
    enabled: isDid && !!org && open,
  });

  if (!isDid) return <>{children}</>;

  // A DID that isn't an org member comes back as a bare { did }, so a response
  // without any identifying field means "no profile", not a profile that
  // happens to be empty.
  const profile = data?.handle || data?.displayName ? data : undefined;

  return (
    <HoverCard open={open} onOpenChange={setOpen}>
      <HoverCardTrigger
        render={<span className={className}>{children}</span>}
      />
      <HoverCardContent>
        {profile ? (
          <div className="flex flex-col gap-2">
            <UserAvatar actor={profile} size="lg" link />
            <div className="flex flex-col">
              {profile.displayName && (
                <span className="font-medium">{profile.displayName}</span>
              )}
              {profile.handle && (
                <span className="text-muted-foreground">@{profile.handle}</span>
              )}
            </div>
            <span className="font-mono text-xs break-all text-muted-foreground">
              {did}
            </span>
          </div>
        ) : isError || data ? (
          <span className="text-muted-foreground">
            No profile for <span className="font-mono break-all">{did}</span>
          </span>
        ) : (
          <Spinner />
        )}
      </HoverCardContent>
    </HoverCard>
  );
}
