import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/forwarding-test")({
  async loader({ context }) {
    const authInfo = context.authManager.getAuthInfo();
    const resp = await context.authManager.fetch(
      `/xrpc/app.bsky.actor.getProfile?actor=${authInfo?.did}`,
    );
    const data: {
      displayName: string;
      avatar: string;
      handle: string;
      followersCount: number;
    } = await resp?.json();
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
        <span>{data.followersCount} followers</span>
      </div>
    );
  },
});
