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
import { profileQueryOptions } from "@/queries/profile";

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
    await context.queryClient.prefetchQuery(
      docsListQueryOptions(context.authManager),
    );
    return { profile, did };
  },
  component() {
    const { profile } = Route.useLoaderData();
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
      mutationFn: (name: string) => createDoc(authManager, name),
      onSuccess: ({ docId }) => {
        queryClient.invalidateQueries(docsListQueryOptions(authManager));
        router.invalidate();
        navigate({ to: "/$uri", params: { uri: docId } });
      },
      onError: (error) => {
        console.error("failed to create doc", error);
        alert(error.message);
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
                onClick={() => create("Untitled")}
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
                          <span>{doc.name}</span>
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
