import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useRouter } from "@tanstack/react-router";
import { OrgAvatar } from "internal";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  toast,
} from "internal/components/ui";
import { Check, ChevronsUpDown, Settings2 } from "lucide-react";
import { listMyOrgs, switchOrg } from "@/server/functions";

// OrgSwitcher sits in the header's corner: the current org, and a menu to
// jump to any other connected org without going back through the picker.
export function OrgSwitcher({
  currentOrg,
}: {
  currentOrg: { did: string; name: string | null };
}) {
  const router = useRouter();
  const { data: orgs } = useQuery({
    queryKey: ["orgs"],
    queryFn: () => listMyOrgs(),
  });
  const connected = (orgs ?? []).filter((o) => o.connected);

  const { mutate: switchTo } = useMutation({
    mutationFn: (orgDid: string) => switchOrg({ data: { orgDid } }),
    onSuccess: () => router.invalidate(),
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't switch organization",
        description: error.message,
      }),
  });

  const label = currentOrg.name ?? currentOrg.did;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex max-w-64 items-center gap-2 rounded-full border border-border bg-background py-1 pr-3 pl-1 text-sm font-medium shadow-xs transition-colors outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50">
        <OrgAvatar did={currentOrg.did} name={label} size="sm" />
        <span className="truncate">{label}</span>
        <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Organizations</DropdownMenuLabel>
          {connected.map((org) => {
            const isCurrent = org.did === currentOrg.did;
            return (
              <DropdownMenuItem
                key={org.did}
                disabled={isCurrent}
                onClick={() => switchTo(org.did)}
              >
                <OrgAvatar did={org.did} name={org.name ?? org.did} size="sm" />
                <span className="truncate">{org.name ?? org.did}</span>
                {isCurrent && <Check className="ml-auto" />}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem render={<Link to="/orgs" />}>
          <Settings2 />
          All organizations
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
