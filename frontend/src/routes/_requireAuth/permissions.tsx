import { createFileRoute, Link, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions")({
  loader() {
    // fetch user permissions
    return {};
  },
  component() {
    return (
      <>
        <h1>Permissions</h1>
        <nav>
          <ul>
            <li>
              <Link to="/permissions/lexicons">Lexicons</Link>
            </li>
            <li>
              <Link to="/permissions/people">People</Link>
            </li>
            <li>
              <Link to="/permissions/groups">Groups</Link>
            </li>
          </ul>
        </nav>
        <Outlet />
      </>
    );
  },
});
