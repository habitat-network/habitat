import { Link } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import type { AuthManager } from "internal/authManager.ts";

function formatHandle(handle: string | null) {
  if (!handle) return "";
  const parts = handle.split(".");
  if (parts.length > 1) {
    return `${parts[0]}@${parts.slice(1).join(".")}`;
  }
  return handle;
}

const Header = ({ authManager }: { authManager: AuthManager }) => {
  const [isAuthenticated, setIsAuthenticated] = useState(
    authManager.isAuthenticated()
  );
  const [handle, setHandle] = useState(authManager.handle);

  useEffect(() => {
    setIsAuthenticated(authManager.isAuthenticated());
    setHandle(authManager.handle);
  }, [authManager]);

  const handleLogout = () => {
    authManager.logout();
    setIsAuthenticated(false);
    setHandle(null);
  };

  return (
    <header>
      <nav>
        <ul>
          <li>
            <Link to="/">🌱 Habitat</Link>
          </li>
        </ul>
        {isAuthenticated && (
          <ul>
            <li>{handle && formatHandle(handle)}</li>
            <li>
              <button onClick={handleLogout}>Logout</button>
            </li>
          </ul>
        )}
      </nav>
    </header>
  );
};

export default Header;
