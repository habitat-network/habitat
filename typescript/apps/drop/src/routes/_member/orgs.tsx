import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { OrgAvatar } from "internal";
import {
  Badge,
  Button,
  Card,
  CardContent,
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { ArrowRight, Building2, Link2 } from "lucide-react";
import { useState } from "react";
import {
  getCurrentOrg,
  listMyOrgs,
  startOrgConnect,
  switchOrg,
} from "@/server/functions";

// The org picker: shown right after sign-in, and from the org switcher's
// "All organizations". A connected org (one this deployment holds the org's
// own session for) is one click to open; any other needs an org admin to
// connect it first, via pear's admin-approval flow.
export const Route = createFileRoute("/_member/orgs")({
  // Loader data rather than useQuery for the same reason as chalk's /orgs:
  // there's no Query SSR hydration wiring, and the list is one-shot.
  loader: async () => {
    const [orgs, currentOrg] = await Promise.all([
      listMyOrgs(),
      getCurrentOrg(),
    ]);
    return { orgs, currentOrg };
  },
  errorComponent: ({ error }) => (
    <p className="mx-auto max-w-lg py-16 text-sm text-destructive">
      Couldn't load your organizations:{" "}
      {error instanceof Error ? error.message : String(error)}
    </p>
  ),
  component() {
    const { orgs, currentOrg } = Route.useLoaderData();
    const navigate = Route.useNavigate();
    const router = useRouter();
    const [pendingDid, setPendingDid] = useState<string | null>(null);

    const { mutate: open } = useMutation({
      mutationFn: async (org: { did: string; connected: boolean }) => {
        setPendingDid(org.did);
        if (!org.connected) {
          const { redirectUrl } = await startOrgConnect({
            data: { orgDid: org.did },
          });
          window.location.href = redirectUrl;
          await new Promise(() => {});
        }
        await switchOrg({ data: { orgDid: org.did } });
      },
      onSuccess: async () => {
        await router.invalidate();
        await navigate({ to: "/" });
      },
      onError: (error) => {
        setPendingDid(null);
        toast.add({
          type: "error",
          title: "Couldn't open this organization",
          description: error.message,
        });
      },
    });

    return (
      <div className="mx-auto flex w-full max-w-lg flex-col gap-6 px-6 py-16">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-2xl font-semibold tracking-tight">
            Choose an organization
          </h1>
          <p className="text-sm text-muted-foreground">
            Files you drop are shared with everyone in the organization you're
            working in. You can switch any time from the top-right corner.
          </p>
        </div>

        {orgs.length === 0 ? (
          <Card>
            <CardContent className="flex flex-col items-center gap-3 py-10 text-center">
              <span className="flex size-11 items-center justify-center rounded-full bg-muted">
                <Building2 className="size-5 text-muted-foreground" />
              </span>
              <div className="flex flex-col gap-1">
                <p className="font-medium">You're not in any organizations</p>
                <p className="text-sm text-muted-foreground">
                  Ask an admin to invite you, then come back here.
                </p>
              </div>
            </CardContent>
          </Card>
        ) : (
          <ItemGroup className="gap-2">
            {orgs.map((org) => {
              const name = org.name ?? org.did;
              const isCurrent = org.did === currentOrg?.did;
              return (
                <Item key={org.did} variant="outline" className="bg-card">
                  <ItemMedia>
                    <OrgAvatar did={org.did} name={name} size="lg" />
                  </ItemMedia>
                  <ItemContent className="min-w-0">
                    <ItemTitle className="flex items-center gap-2">
                      <span className="truncate">{name}</span>
                      {isCurrent && <Badge variant="secondary">Current</Badge>}
                    </ItemTitle>
                    <ItemDescription>
                      {org.connected
                        ? "Connected to Drop"
                        : "An org admin needs to connect it to Drop"}
                    </ItemDescription>
                  </ItemContent>
                  <ItemActions>
                    <Button
                      variant={org.connected ? "default" : "outline"}
                      loading={pendingDid === org.did}
                      disabled={pendingDid !== null}
                      onClick={() => open(org)}
                    >
                      {org.connected ? (
                        <>
                          Open
                          <ArrowRight data-icon="inline-end" />
                        </>
                      ) : (
                        <>
                          <Link2 data-icon="inline-start" />
                          Connect
                        </>
                      )}
                    </Button>
                  </ItemActions>
                </Item>
              );
            })}
          </ItemGroup>
        )}
      </div>
    );
  },
});
