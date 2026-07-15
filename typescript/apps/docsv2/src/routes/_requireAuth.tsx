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
  async beforeLoad({ context }) {
    await context.authManager.init();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
  },
  async loader({ context }) {
    const did = context.authManager.getAuthInfo()!.did;
    await context.queryClient.prefetchQuery(
      docsListQueryOptions(context.authManager),
    );
    return { did };
  },
  component() {
    const { did } = Route.useLoaderData();
    const { authManager, queryClient } = Route.useRouteContext();
    const { data: docs } = useQuery(docsListQueryOptions(authManager));
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentDocId = useRouterState({
      select: (state) =>
        state.matches.find((x) => x.routeId === "/_requireAuth/$uri")?.params
          .uri,
    });

    const { mutate: create, isPending } = useMutation({
      mutationFn: () => createDoc(authManager),
      onSuccess: async ({ docId }) => {
        // Refresh the list so the editor route can resolve the new doc's space
        // URI from it before navigating.
        await queryClient.invalidateQueries(docsListQueryOptions(authManager));
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
        onSignOut={() => authManager.logout()}
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
