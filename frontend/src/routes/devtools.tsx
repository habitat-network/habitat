import { createFileRoute, Link } from "@tanstack/react-router";

export const Route = createFileRoute("/devtools")({
  async loader() {
    return [
      {
        id: "permissions",
        name: "Permissions",
        description: "Manage permissions for pear",
        icon: "🔑",
        link: "/permissions",
      },
      {
        id: "pear-test",
        name: "Pear Test",
        description: "Pear Test for getting and putting records",
        icon: "💿",
        link: "/pear-test",
      },
      {
        id: "blob-test",
        name: "Blob Test",
        description: "Test uploading / getting blobs",
        icon: "📸",
        link: "/blob-test",
      },
      {
        id: "forwarding-test",
        name: "Forwarding Test",
        description: "Test forwarding",
        icon: "🦋",
        link: "/forwarding-test",
      },
      {
        id: "data",
        name: "Data Debugger",
        description: "Browse and filter records by lexicon",
        icon: "🗄️",
        link: "/data",
      },
      {
        id: "onboard",
        name: "Habitat Onboarding (DID updater)",
        description: "Join Habitat by updating DID",
        icon: "🍾",
        link: "/onboard",
      },
      {
        id: "jetstream",
        name: "Jetstream",
        description: "Live SSE event stream from the Jetstream server",
        icon: "📡",
        link: "/jetstream",
      },
    ];
  },
  component() {
    const data = Route.useLoaderData();
    return (
      <>
        <h1>Shortcuts</h1>
        <table>
          <thead>
            <tr>
              <th>App</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {data.map(({ id, name, description, icon, link }) => (
              <tr key={id}>
                <td>
                  <Link to={link}>
                    {icon} {name}
                  </Link>
                </td>
                <td>{description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </>
    );
  },
});
