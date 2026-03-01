import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/explore")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/explore"! This is not implemented yet. </div>;
}
