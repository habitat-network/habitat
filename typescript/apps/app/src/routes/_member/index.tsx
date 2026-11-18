import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Button } from "internal/components/ui";
import { signOut } from "@/server/functions";

export const Route = createFileRoute("/_member/")({ component: Home });

function Home() {
  const { member } = Route.useRouteContext();
  const navigate = useNavigate();
  return (
    <div className="p-8">
      <h1 className="text-2xl font-semibold">
        Signed in as {member.handle ?? member.did}
      </h1>
      <Button
        className="mt-4"
        variant="outline"
        onClick={async () => {
          await signOut();
          await navigate({ to: "/login" });
        }}
      >
        Sign out
      </Button>
    </div>
  );
}
