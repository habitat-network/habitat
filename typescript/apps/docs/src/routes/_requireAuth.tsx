import { docsListQueryOptions } from "@/queries/docs";
import { profileQueryOptions } from "@/queries/profile";
import {
  createFileRoute,
  Link,
  Outlet,
  redirect,
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
import { FileTextIcon } from "lucide-react";

export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
  },
  async loader({ context }) {
    const did = context.authManager.getAuthInfo()!.did;
    const profile = await context.queryClient.ensureQueryData(
      profileQueryOptions(did, context.authManager),
    );
    const docs = await context.queryClient.ensureQueryData(
      docsListQueryOptions(context.authManager),
    );
    return { profile, docs };
  },
  component() {
    const { profile, docs } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    return (
      <AppLayout
        actor={profile}
        onSignOut={() => authManager.logout()}
        title="Habitat Docs"
        sidebar={
          <SidebarGroup>
            <SidebarGroupLabel>Documents</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {docs.records.map((doc) => (
                  <SidebarMenuItem key={doc.uri}>
                    <SidebarMenuButton
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
        }
      >
        <Outlet />
      </AppLayout>
    );
  },
});
