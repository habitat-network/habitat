import { createFileRoute, Link } from "@tanstack/react-router";

const pages = [
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
];

export const Route = createFileRoute("/")({
  async beforeLoad({ search, context }) {
    if ("code" in search) {
      await context.authManager.exchangeCode(window.location.href);
      window.location.search = "";
      try {
        const url = new URL(window.location.href);
        url.searchParams.delete("code");
        window.history.replaceState({}, "", url.toString());
      } catch {
        window.location.search = "";
      }
    }
  },
  component() {
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
            {pages.map(({ id, name, description, icon, link }) => (
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
