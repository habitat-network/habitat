import { createFileRoute } from "@tanstack/react-router";
import { getProfile } from "internal";

export const Route = createFileRoute("/_requireAuth/forwarding-test")({
  async loader({ context }) {
    const authInfo = context.authManager.getAuthInfo();
    const data = await getProfile(authInfo?.did ?? "");
    return { data };
  },
  component() {
    const { data } = Route.useLoaderData();
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          flexDirection: "column",
          gap: 8,
        }}
      >
        <img
          src={data.avatar}
          style={{ width: 50, height: 50, borderRadius: "50%" }}
        />
        {data.displayName || data.handle}
      </div>
    );
  },
});
