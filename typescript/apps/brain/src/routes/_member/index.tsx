import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_member/")({
  component() {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-center text-muted-foreground">
        Start a new chat from the sidebar.
      </div>
    );
  },
});
