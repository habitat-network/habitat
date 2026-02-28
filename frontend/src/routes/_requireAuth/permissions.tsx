import { createFileRoute, Link, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/permissions")({
  component() {
    return (
      <>
        <h1>Permissions</h1>
        <nav>
          <ul>
            <li>
              <Link to="/permissions/lexicons">By collection</Link>
            </li>
            <li>
              <Link to="/permissions/people">By person</Link>
            </li>

          </ul>
        </nav>
        <Outlet />
      </>
    );
  },
});
