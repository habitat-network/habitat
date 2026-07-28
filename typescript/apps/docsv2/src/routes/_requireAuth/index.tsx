import { createFileRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/PageHeader";

export const Route = createFileRoute("/_requireAuth/")({
  component() {
    return (
      <div className="h-full w-full flex flex-col">
        <PageHeader />
        <div className="flex-1 flex items-center justify-center">
          Welcome to Habitat Docs!
        </div>
      </div>
    );
  },
});
