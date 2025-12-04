import { createFileRoute } from "@tanstack/react-router";

interface Notification {
  uri: string;
  cid: string;
  value: {
    originDid: string;
    collection: string;
    rkey: string;
  };
}

interface ListNotificationsResponse {
  records: Notification[];
  cursor?: string;
}

export const Route = createFileRoute("/_requireAuth/notifications")({
  async loader({ context }) {
    const params = new URLSearchParams();
    params.set("limit", "50");

    const response = await context.authManager.fetch(
      `https://${__HABITAT_DOMAIN__}/xrpc/network.habitat.notification.listNotifications?${params.toString()}`,
      "GET"
    );

    if (!response.ok) {
      throw new Error("Failed to fetch notifications");
    }

    const data: ListNotificationsResponse = await response.json();
    return { data };
  },
  component() {
    const { data } = Route.useLoaderData();

    return (
      <article>
        <h1>Notifications</h1>

        {data.records.length === 0 && (
          <p>No notifications found.</p>
        )}

        {data.records.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>Origin DID</th>
                <th>Collection</th>
                <th>Record Key</th>
                <th>URI</th>
              </tr>
            </thead>
            <tbody>
              {data.records.map((notification) => (
                <tr key={notification.uri}>
                  <td>{notification.value.originDid}</td>
                  <td>{notification.value.collection}</td>
                  <td>{notification.value.rkey}</td>
                  <td style={{ fontSize: "0.8em", wordBreak: "break-all" }}>
                    {notification.uri}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </article>
    );
  },
});

