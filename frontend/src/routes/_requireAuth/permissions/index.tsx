import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions/")({
  loader() {
    throw redirect({ to: "/permissions/lexicons" });
  },
});
