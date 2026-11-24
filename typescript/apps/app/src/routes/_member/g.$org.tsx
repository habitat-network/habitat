import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { Button, toast } from "internal/components/ui";
import { Unplug } from "lucide-react";
import { groupLabel, groupQuery } from "@/lib/queries";
import { startGroupConnect } from "@/server/fns/auth";

// Layout for one group's pages. Loads the group (and whether the member
// belongs to it: getGroup fails NotAMember otherwise) and, until an admin
// has connected the group to the app, says so above every page.
export const Route = createFileRoute("/_member/g/$org")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(groupQuery(params.org)),
  component: GroupLayout,
  errorComponent: ({ error }) => (
    <div className="p-8">
      <h1 className="text-lg font-semibold">Couldn't open this group</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        {error instanceof Error ? error.message : String(error)}
      </p>
    </div>
  ),
});

function GroupLayout() {
  const { org } = Route.useParams();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  return (
    <>
      {!group.connected && (
        <ConnectBanner
          org={org}
          name={groupLabel(group)}
          isAdmin={group.myActions.includes("community.configure")}
        />
      )}
      <Outlet />
    </>
  );
}

// ConnectBanner explains that docs and files need the group connected: the
// app holds the group's own session to create and share its spaces. Only
// an admin can approve that on pear's side.
function ConnectBanner({
  org,
  name,
  isAdmin,
}: {
  org: string;
  name: string;
  isAdmin: boolean;
}) {
  const connect = useMutation({
    mutationFn: () => startGroupConnect({ data: { org } }),
    onSuccess: ({ redirectUrl }) => {
      window.location.href = redirectUrl;
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't connect the group",
        description: e.message,
      }),
  });
  return (
    <div className="flex items-center gap-3 border-b bg-muted/50 px-4 py-2 text-sm">
      <Unplug className="size-4 shrink-0 text-muted-foreground" />
      <p className="flex-1">
        {isAdmin
          ? `Connect ${name} to this app to create docs and upload files.`
          : `An admin needs to connect ${name} before docs and files can be added.`}
      </p>
      {isAdmin && (
        <Button
          size="sm"
          onClick={() => connect.mutate()}
          disabled={connect.isPending}
        >
          Connect group
        </Button>
      )}
    </div>
  );
}
