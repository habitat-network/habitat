import { Link } from "@tanstack/react-router";
import { useAuth } from "./authContext";

function formatHandle(handle: string | null) {
  if (!handle) return "";
  const parts = handle.split(".");
  if (parts.length > 1) {
    return `${parts[0]}@${parts.slice(1).join(".")}`;
  }
  return handle;
}

const Header = () => {
  const { isAuthenticated, handle, logout } = useAuth();
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
              <button onClick={logout}>Logout</button>
            </li>
          </ul>
        )}
      </nav>
    </header>
  );
};

export default Header;
