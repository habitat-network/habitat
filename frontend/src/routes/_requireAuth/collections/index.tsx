import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/collections/")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/_requireAuth/collections/"!</div>;
}
