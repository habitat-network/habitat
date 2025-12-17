import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/")({
  component() {
    return "{{name}}";
  },
});
