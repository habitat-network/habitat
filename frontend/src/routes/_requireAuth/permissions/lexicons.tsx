import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions/lexicons")({
  loader() {
    // fetch user permissions
    return {};
  },
  component() {
    return (
      <>
        <h2>NSIDs</h2>
        <Outlet />
      </>
    );
  },
});
