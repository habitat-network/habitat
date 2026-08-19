import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createFileRoute,
  Link,
  Outlet,
  redirect,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
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
import type { DocSummary } from "@/server/docStore";
import { useAppSession } from "@/server/session";

// Route-local auth guard: resolves the caller's DID for the layout (and
// redirects to /login when there isn't one) without depending on
// functions.ts's requireSession, whose redirect target is cast to "." there
// because /login isn't registered yet in that file's scope.
const getCallerFn = createServerFn({ method: "GET" }).handler(async () => {
  const session = await useAppSession();
  if (!session.data.did) {
    throw redirect({ to: "/login" });
  }
  return { did: session.data.did };
});

// createDoc/listDocs live in functions.ts alongside two plain (non
// createServerFn) exports, requireSession and subscribeDoc, which still
// call useAppSession()/useSession() directly. A static top-level
// `import { createDoc, listDocs } from "@/server/functions"` here would
// pull that whole module — including those exports' server-only imports —
// into the client bundle graph, which the build's import-protection plugin
// rejects (it denies any client-reachable module importing
// "@tanstack/react-start/server", regardless of whether the specific
// binding used is itself safe). A *dynamic* import inside each handler
// keeps the reference inside the extracted server-only chunk instead, so
// only these RPC stubs — not functions.ts's other exports — are reachable
// from the client.
const createDocFn = createServerFn({ method: "POST" }).handler(async () => {
  const { createDoc } = await import("@/server/functions");
  return createDoc();
});

const listDocsFn = createServerFn({ method: "GET" }).handler(
  async (): Promise<DocSummary[]> => {
    const { listDocs } = await import("@/server/functions");
    return listDocs();
  },
);

export const Route = createFileRoute("/_requireAuth")({
  beforeLoad: async () => await getCallerFn(),
  loader: async ({ context }) => {
    await context.queryClient.prefetchQuery({
      queryKey: ["docs"],
      queryFn: () => listDocsFn(),
    });
  },
  component() {
    const { did } = Route.useRouteContext();
    const queryClient = useQueryClient();
    const { data: docs } = useQuery({
      queryKey: ["docs"],
      queryFn: () => listDocsFn(),
    });
    const router = useRouter();
    const navigate = Route.useNavigate();

    const currentDocId = useRouterState({
      select: (state) =>
        state.matches.find((x) => x.routeId === "/_requireAuth/$uri")?.params
          .uri,
    });

    const { mutate: create, isPending } = useMutation({
      mutationFn: () => createDocFn(),
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

    return (
      <AppLayout
        actor={{ did }}
        title="Chalk"
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
