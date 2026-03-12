import { procedure } from "internal";

import { useMutation } from "@tanstack/react-query";
import { docsListQueryOptions } from "@/queries/docs";
import { profileQueryOptions } from "@/queries/profile";
import {
  createFileRoute,
  Link,
  Outlet,
  redirect,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import {
  AppLayout,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from "internal";
import { FileTextIcon, PlusIcon } from "lucide-react";


export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
  },
  async loader({ context }) {
    const did = context.authManager.getAuthInfo()!.did;
    const profile = await context.queryClient.fetchQuery(
      profileQueryOptions(did, context.authManager),
    );
    const docs = await context.queryClient.fetchQuery(
      docsListQueryOptions(context.authManager),
    );
    return { profile, docs };
  },
  component() {
    const { profile, docs } = Route.useLoaderData();
    const { authManager, queryClient } = Route.useRouteContext();
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentUri = useRouterState({
      select: (state) => {
        return state.matches.find((x) => x.routeId === "/_requireAuth/$uri")
          ?.params.uri;
      },
    });

    const { mutate: create, isPending } = useMutation({
      mutationFn: async () => {
        const did = authManager.getAuthInfo()?.did;
        const response = await procedure(
          "network.habitat.putRecord",
          {
            repo: did ?? "",
            collection: "network.habitat.docs",
            record: {
              name: "Untitled",
              blob: null,
            },
          },
          { authManager },
        );
        navigate({
          to: "/$uri",
          params: {
            uri: response.uri,
          },
        });
        router.invalidate({ filter: (x) => x.pathname === "/docs/" });
      },
      onSuccess: () => {
        queryClient.invalidateQueries(docsListQueryOptions(authManager));
        router.invalidate();
      },
    });
    return (
      <AppLayout
        actor={profile}
        onSignOut={() => authManager.logout()}
        title="Habitat Docs"
        sidebar={
          <>
            <SidebarGroup>
              <SidebarMenuButton
                variant="outline"
                onClick={() => {
                  create();
                }}
                disabled={isPending}
              >
                <PlusIcon />
                New Document
              </SidebarMenuButton>
            </SidebarGroup>
            <SidebarGroup>
              <SidebarGroupLabel>Documents</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {docs.records.map((doc) => (
                    <SidebarMenuItem key={doc.uri}>
                      <SidebarMenuButton
                        isActive={currentUri === doc.uri}
                        render={<Link to="/$uri" params={{ uri: doc.uri }} />}
                      >
                        <FileTextIcon />
                        <span>
                          {!doc.value.name || doc.value.name === "Untitled"
                            ? `Untitled (${doc.uri.split("/")[4]})`
                            : doc.value.name}
                        </span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </>
        }
      >
        <Outlet />
      </AppLayout>
    );
  },
});
