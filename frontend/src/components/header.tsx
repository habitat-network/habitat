import { Link } from "@tanstack/react-router";
import type { AuthManager } from "internal/authManager";

function formatHandle(handle: string | null) {
  if (!handle) return "";
  const parts = handle.split(".");
  if (parts.length > 1) {
    return `${parts[0]}@${parts.slice(1).join(".")}`;
  }
  return handle;
}

const Header = ({ authManager }: { authManager: AuthManager }) => {
  return (
    <header>
      <nav>
        <ul>
          <li>
            <Link to="/">🌱 Habitat</Link>
          </li>
        </ul>
        {authManager.isAuthenticated() && (
          <ul>
            <li>{authManager.handle && formatHandle(authManager.handle)}</li>
            <li>
              <button onClick={authManager.logout}>
                Logout [does nothing right now]
              </button>
            </li>
          </ul>
        )}
      </nav>
    </header>
  );
};

export default Header;
