import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_member/g/$org/")({
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/g/$org/files", params });
  },
});
