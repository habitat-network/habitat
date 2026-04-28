import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/login/habitat")({
  component() {
    useEffect(() => {
      window.location.replace(`https://${__HABITAT_DOMAIN__}/oauth-callback`);
    }, []);
    return <p>Logging in...</p>;
  },
});
