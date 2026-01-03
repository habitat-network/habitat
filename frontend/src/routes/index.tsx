import { createFileRoute, Link } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode(window.location.href);
  },
  async loader() {
    return [
      {
        id: "permissions",
        name: "Permissions",
        description: "Manage permissions for privi",
        icon: "🔑",
        link: "/permissions",
      },
      {
        id: "privi-test",
        name: "Privi Test",
        description: "Privi Test for getting and putting records",
        icon: "💿",
        link: "/privi-test",
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
        id: "notifications",
        name: "Notifications",
        description: "View your notifications",
        icon: "🔔",
        link: "/notifications",
      },
      {
        id: "data",
        name: "Data Debugger",
        description: "Browse and filter records by lexicon",
        icon: "🗄️",
        link: "/data",
      },
    ];
  },
  component: Wrapper,
});

function Wrapper() {
  const { authManager } = Route.useRouteContext();
  return authManager.isAuthenticated() ? (
    <Shortcuts />
  ) : (
    <Link to="/oauth-login">Login</Link>
  );
}

function Shortcuts() {
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
}
