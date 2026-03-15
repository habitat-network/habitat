import { procedure, TypedRecord } from "internal";

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
import { HabitatDoc } from "@/habitatDoc";

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
    const userDocs = docs.records.filter((d) => d.uri.includes(did));
    const sharedDocs = docs.records.filter((d) => !d.uri.includes(did));
    return { profile, userDocs, sharedDocs };
  },
  component() {
    const { profile, userDocs, sharedDocs } = Route.useLoaderData();
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
            {userDocs.length > 0 && (
              <SidebarGroup>
                <SidebarGroupLabel>My documents</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {userDocs.map((doc) => (
                      <DocItem
                        key={doc.uri}
                        doc={doc}
                        isActive={currentUri === doc.uri}
                      />
                    ))}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            )}
            {sharedDocs.length > 0 && (
              <SidebarGroup>
                <SidebarGroupLabel>Shared with me</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {sharedDocs.map((doc) => (
                      <DocItem
                        key={doc.uri}
                        doc={doc}
                        isActive={currentUri === doc.uri}
                      />
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

const DocItem = ({
  doc,
  isActive,
}: {
  doc: TypedRecord<HabitatDoc>;
  isActive: boolean;
}) => (
  <SidebarMenuItem>
    <SidebarMenuButton
      isActive={isActive}
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
);
