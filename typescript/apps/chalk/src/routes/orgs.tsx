import { createFileRoute } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import {
  Button,
  Item,
  ItemContent,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { listMyOrgs, startOrgConnect } from "@/server/functions";

export const Route = createFileRoute("/orgs")({
  // Fetched once in the loader and read via Route.useLoaderData() below,
  // not useQuery: this app has no TanStack Query SSR dehydrate/hydrate
  // wiring, so a useQuery here would start from an empty client-side
  // cache and mismatch the server-rendered HTML on hydration. The
  // loader's own data crosses the SSR boundary correctly (TanStack
  // Router serializes it directly), and a one-shot org list has no need
  // for useQuery's background refetch/invalidation machinery anyway.
  loader: () => listMyOrgs(),
  errorComponent: ({ error }) => (
    <p className="text-sm text-destructive">
      Couldn't load your orgs: {error.message}
    </p>
  ),
  component() {
    const orgs = Route.useLoaderData();
    const { mutate: connect, isPending } = useMutation({
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

    return (
      <div className="flex w-full max-w-md flex-col gap-2 py-16 mx-auto">
        <h1 className="text-lg font-semibold">Connect an org</h1>
        {orgs.length === 0 && (
          <p className="text-sm text-muted-foreground">
            You don't belong to any orgs yet.
          </p>
        )}
        {orgs.map((org) => (
          <Item key={org.did} variant="outline">
            <ItemContent>
              <ItemTitle>{org.name ?? org.did}</ItemTitle>
            </ItemContent>
            <Button disabled={isPending} onClick={() => connect(org.did)}>
              Connect
            </Button>
          </Item>
        ))}
      </div>
    );
  },
});
