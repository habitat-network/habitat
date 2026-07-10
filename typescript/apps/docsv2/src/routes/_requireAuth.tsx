import { useMutation, useQuery } from "@tanstack/react-query";
import { createDoc, docsListQueryOptions } from "@/queries/docs";
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
  // Auth is the docs server's server session; whoami confirms it and yields the
  // logged-in DID (placed on the route context for the component and loader).
  async beforeLoad({ context }) {
    const did = await context.fetcher.whoami();
    if (!did) {
      throw redirect({ to: "/login" });
    }
    return { did };
  },
  async loader({ context }) {
    await context.queryClient.prefetchQuery(
      docsListQueryOptions(context.fetcher),
    );
  },
  component() {
    const { did, fetcher, queryClient } = Route.useRouteContext();
    const { data: docs } = useQuery(docsListQueryOptions(fetcher));
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentDocId = useRouterState({
      select: (state) =>
        state.matches.find((x) => x.routeId === "/_requireAuth/$uri")?.params
          .uri,
    });

    const { mutate: create, isPending } = useMutation({
      mutationFn: () => createDoc(fetcher),
      onSuccess: async ({ docId }) => {
        // Refresh the list so the editor route can resolve the new doc's space
        // URI from it before navigating.
        await queryClient.invalidateQueries(docsListQueryOptions(fetcher));
        router.invalidate();
        navigate({ to: "/$uri", params: { uri: docId } });
      },
      onError: (error) => {
        console.error("failed to create doc", error);
      },
    });

    return (
      <AppLayout
        actor={{ did }}
        onSignOut={() => fetcher.logout()}
        title="Habitat Docs"
        sidebar={
          <>
            <SidebarGroup>
              <SidebarMenuButton
                variant="outline"
                className="bg-sidebar-primary/10 hover:bg-sidebar-primary/20 border-sidebar-primary/30 text-sidebar-primary font-medium"
                onClick={() => create()}
                disabled={isPending}
              >
                <PlusIcon />
                New Document
              </SidebarMenuButton>
            </SidebarGroup>
            {docs && docs.length > 0 && (
              <SidebarGroup>
                <SidebarGroupLabel>Documents</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {docs.map((doc) => (
                      <SidebarMenuItem key={doc.docId}>
                        <SidebarMenuButton
                          isActive={currentDocId === doc.docId}
                          render={
                            <Link to="/$uri" params={{ uri: doc.docId }} />
                          }
                        >
                          <FileTextIcon />
                          <span>{doc.title}</span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            )}
          </>
        }
      >
        <Outlet />
      </AppLayout>
    );
  },
});
