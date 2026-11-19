import { createFileRoute, Outlet } from "@tanstack/react-router";
import { getMember } from "@/server/functions";

// Layout for everything behind sign-in: beforeLoad redirects to /login when
// there is no session, and puts the signed-in member in route context.
export const Route = createFileRoute("/_member")({
  beforeLoad: async () => ({ member: await getMember() }),
  component: Outlet,
});
