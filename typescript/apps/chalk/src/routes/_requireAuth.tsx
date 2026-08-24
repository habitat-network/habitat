import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createFileRoute,
  Link,
  Outlet,
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
import { toast } from "internal/components/ui";
import { FileTextIcon, PlusIcon } from "lucide-react";
import { createDoc, getCaller, listDocs, signOut } from "@/server/functions";

export const Route = createFileRoute("/_requireAuth")({
  beforeLoad: async () => await getCaller(),
  loader: async ({ context }) => {
    await context.queryClient.prefetchQuery({
      queryKey: ["docs"],
      queryFn: () => listDocs(),
    });
  },
  component() {
    const { did } = Route.useRouteContext();
    const queryClient = useQueryClient();
    const { data: docs } = useQuery({
      queryKey: ["docs"],
      queryFn: () => listDocs(),
    });
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentDocId = useRouterState({
      select: (state) =>
        state.matches.find((x) => x.routeId === "/_requireAuth/$uri")?.params
          .uri,
    });

    const { mutate: create, isPending } = useMutation({
      mutationFn: () => createDoc(),
      onSuccess: async ({ docId }) => {
        // Refresh the list so the editor route can resolve the new doc's
        // space URI from it before navigating.
        await queryClient.invalidateQueries({ queryKey: ["docs"] });
        router.invalidate();
        navigate({ to: "/$uri", params: { uri: docId } });
      },
      onError: (error) => {
        console.error("failed to create doc", error);
      },
    });

    const { mutate: logOut } = useMutation({
      mutationFn: () => signOut(),
      onSuccess: async () => {
        await router.navigate({ to: "/login" });
        queryClient.clear();
        await router.invalidate();
      },
      onError: (error) => {
        toast.add({
          type: "error",
          title: "Couldn't sign out",
          description: error.message,
        });
      },
    });

    return (
      <AppLayout
        actor={{ did }}
        title="Chalk"
        onSignOut={() => logOut()}
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
