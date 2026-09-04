import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createFileRoute,
  Link,
  Outlet,
  useLocation,
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
import { HomeIcon, PlusIcon, ArrowLeftRight } from "lucide-react";
import {
  createDoc,
  getCaller,
  getCurrentOrg,
  listDocs,
  signOut,
} from "@/server/functions";
import { useRecentDocsStore } from "@/stores/recentDocs";
import type { DocSummary } from "@/db";

export const Route = createFileRoute("/_requireAuth")({
  beforeLoad: async () => await getCaller(),
  loader: async ({ context }) => {
    const [, actor, currentOrg] = await Promise.all([
      // Seed the ["docs"] query cache the sidebar/home page share, so the
      // component's useQuery below resolves from cache instead of
      // refetching, while still letting either surface invalidate it (e.g.
      // right after creating a doc, or optimistically as a title changes).
      context.queryClient.ensureQueryData({
        queryKey: ["docs"],
        queryFn: listDocs,
      }),
      // The AppLayout footer needs a resolved handle/avatar to show
      // anything besides "Unknown User" — getCaller only gives us the did.
      getProfile(context.did),
      getCurrentOrg(),
    ]);
    return { actor, currentOrg };
  },
  component() {
    const { actor, currentOrg } = Route.useLoaderData();
    const queryClient = useQueryClient();
    const { data: docs = [] } = useQuery({
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
    const location = useLocation()
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
      onSuccess: ({ docId, uri }) => {
        // Append the new doc to the shared ["docs"] cache directly (its
        // fields mirror what src/server/functions.ts's createDoc handler
        // just wrote server-side) rather than invalidating and refetching.
        queryClient.setQueryData<DocSummary[]>(["docs"], (old) => [
          ...(old ?? []),
          {
            docId,
            uri,
            ownerDid: currentOrg?.did ?? actor.did,
            title: "Untitled",
            isOrg: !!currentOrg,
          },
        ]);
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
        footerExtra={
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                isActive={location.pathname === "/orgs"}
                tooltip="Switch organization"
                render={<Link to="/orgs" />}
              >
                <ArrowLeftRight />
                <span>
                  {currentOrg
                    ? (currentOrg.name ?? currentOrg.did)
                    : "Personal"}
                </span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        }
        sidebarHeader={
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                isActive={location.pathname === "/"}
                render={<Link to="/" />}
              >
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
              <SidebarGroup className="group-data-[collapsible=icon]:hidden">
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
