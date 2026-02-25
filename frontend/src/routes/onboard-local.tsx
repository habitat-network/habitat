import { createFileRoute } from "@tanstack/react-router";
import { OnboardComponent } from "./onboard";

export const Route = createFileRoute("/onboard-local")({
  component: () => (
    <OnboardComponent
      serviceKey="habitat_local"
      title="Onboard (Local)"
      defaultServer="https://pear.taile529e.ts.net"
    />
  ),
});
