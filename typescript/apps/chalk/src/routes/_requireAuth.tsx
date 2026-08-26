import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createFileRoute,
  Link,
  Outlet,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import {
  AppLayout,
  getProfile,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from "internal";
import { toast } from "internal/components/ui";
import { HomeIcon, PlusIcon } from "lucide-react";
import { createDoc, getCaller, listDocs, signOut } from "@/server/functions";
import { useRecentDocsStore } from "@/stores/recentDocs";

export const Route = createFileRoute("/_requireAuth")({
  beforeLoad: async () => await getCaller(),
  loader: async ({ context }) => {
    const [docs, actor] = await Promise.all([
      listDocs(),
      // The AppLayout footer needs a resolved handle/avatar to show
      // anything besides "Unknown User" — getCaller only gives us the did.
      getProfile(context.did),
    ]);
    return { docs, actor };
  },
  component() {
    const { docs, actor } = Route.useLoaderData();
    const queryClient = useQueryClient();
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentDocId = useRouterState({
      select: (state) =>
        state.matches.find((x) => x.routeId === "/_requireAuth/$uri")?.params
          .uri,
    });
    const onHomePage = useRouterState({
      select: (state) =>
        state.matches.some((x) => x.routeId === "/_requireAuth/"),
    });
    const recentDocIds = useRecentDocsStore((state) => state.recentDocIds);
    const addRecentDoc = useRecentDocsStore((state) => state.addRecentDoc);
    // recentDocIds only remembers order of visits; docs (the full
    // accessor list) is the source of truth for whether a doc still
    // exists/is still accessible and for its current title.
    const recentDocs = recentDocIds
      .map((docId) => docs.find((doc) => doc.docId === docId))
      .filter((doc) => doc !== undefined);
    const { mutate: create, isPending } = useMutation({
      mutationFn: () => createDoc(),
      onSuccess: async ({ docId }) => {
        // Refresh the loader so it resolves the new doc's space URI
        // before navigating.
        await router.invalidate();
        addRecentDoc(docId);
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
        actor={actor}
        title="Chalk"
        onSignOut={() => logOut()}
        sidebarHeader={
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton isActive={onHomePage} render={<Link to="/" />}>
                <HomeIcon />
                <span>Home</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton
                variant="outline"
                onClick={() => create()}
                disabled={isPending}
              >
                <PlusIcon />
                <span>New Document</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        }
        sidebarContent={
          <>
            {recentDocs.length > 0 && (
              <SidebarGroup className="group-data-[collapsible=icon]:hidden" >
                <SidebarGroupLabel>Recent</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {recentDocs.map((doc) => (
                      <SidebarMenuItem key={doc.docId}>
                        <SidebarMenuButton
                          isActive={currentDocId === doc.docId}
                          render={
                            <Link to="/$uri" params={{ uri: doc.docId }} />
                          }
                        >
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
