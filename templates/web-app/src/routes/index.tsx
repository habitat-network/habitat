import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode(window.location.href);
  },
  component() {
    return "{{name}}";
  },
});
