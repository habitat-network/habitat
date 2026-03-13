import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/")({
  component() {
    return (
      <div className="h-full w-full flex items-center justify-center">
        Welcome to Habitat Docs!
      </div>
    );
  },
});
