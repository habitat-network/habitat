import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Item,
  ItemContent,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { CheckIcon } from "lucide-react";
import {
  getCurrentOrg,
  listMyOrgs,
  startOrgConnect,
  switchOrg,
  switchToPersonal,
} from "@/server/functions";

export const Route = createFileRoute("/_requireAuth/orgs")({
  // Fetched once in the loader and read via Route.useLoaderData() below,
  // not useQuery: this app has no TanStack Query SSR dehydrate/hydrate
  // wiring, so a useQuery here would start from an empty client-side
  // cache and mismatch the server-rendered HTML on hydration. The
  // loader's own data crosses the SSR boundary correctly (TanStack
  // Router serializes it directly), and a one-shot org list has no need
  // for useQuery's background refetch/invalidation machinery anyway.
  loader: async () => {
    const [orgs, currentOrg] = await Promise.all([
      listMyOrgs(),
      getCurrentOrg(),
    ]);
    return { orgs, currentOrg };
  },
  errorComponent: ({ error }) => (
    <p className="text-sm text-destructive">
      Couldn't load your orgs: {error.message}
    </p>
  ),
  component() {
    const { orgs, currentOrg } = Route.useLoaderData();
    const navigate = Route.useNavigate();
    const router = useRouter();

    const { mutate: connect, isPending: isConnecting } = useMutation({
      mutationFn: (orgDid: string) => startOrgConnect({ data: { orgDid } }),
      onSuccess: ({ redirectUrl }) => {
        window.location.href = redirectUrl;
      },
      onError: (error) => {
        toast.add({
          type: "error",
          title: "Couldn't start connecting this org",
          description: error.message,
        });
      },
    });

    // Switching to an already-connected org (or back to Personal) is a
    // plain session write, not the OAuth admin-approval round-trip
    // startOrgConnect kicks off — so both land the member straight back on
    // "/" instead of bouncing through /session/org-callback.
    const { mutate: switchTo, isPending: isSwitching } = useMutation({
      mutationFn: (orgDid: string | undefined) =>
        orgDid ? switchOrg({ data: { orgDid } }) : switchToPersonal(),
      onSuccess: async () => {
        await router.invalidate();
        navigate({ to: "/" });
      },
      onError: (error) => {
        toast.add({
          type: "error",
          title: "Couldn't switch",
          description: error.message,
        });
      },
    });

    const isPending = isConnecting || isSwitching;
    const onPersonal = !currentOrg;

    return (
      <div className="flex w-full max-w-md flex-col gap-2 py-16 mx-auto">
        <h1 className="text-lg font-semibold">Switch organization</h1>
        <Item variant="outline">
          <ItemContent>
            <ItemTitle className="flex items-center gap-2">
              Personal
              {onPersonal && <Badge variant="secondary">Current</Badge>}
            </ItemTitle>
          </ItemContent>
          <Button
            variant="default"
            disabled={isPending || onPersonal}
            onClick={() => switchTo(undefined)}
          >
            Switch
          </Button>
        </Item>

        {orgs.length === 0 && (
          <p className="text-sm text-muted-foreground">
            You don't belong to any orgs yet.
          </p>
        )}
        {orgs.map((org) => {
          const isCurrent = org.did === currentOrg?.did;
          return (
            <Item key={org.did} variant="outline">
              <ItemContent>
                <ItemTitle className="flex items-center gap-2">
                  {org.name ?? org.did}
                  {isCurrent && <Badge variant="secondary">Current</Badge>}
                </ItemTitle>
              </ItemContent>
              <Button
                variant={org.connected ? "default" : "secondary"}
                disabled={isPending || isCurrent}
                onClick={() =>
                  org.connected ? switchTo(org.did) : connect(org.did)
                }
              >
                {org.connected ? "Switch" : "Connect"}
              </Button>
            </Item>
          );
        })}
      </div>
    );
  },
});
