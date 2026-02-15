import { Link } from "@tanstack/react-router";

interface HeaderProps {
  handle: string | null;
  onLogout: () => void;
}

const Header = ({ handle, onLogout }: HeaderProps) => {
  return (
    <header>
      <nav>
        <ul>
          <li>
            <Link to="/">🌱 Habitat</Link>
          </li>
        </ul>
        {handle ? (
          <ul>
            <li>{handle}</li>
            <li>
              <button onClick={onLogout}>Logout</button>
            </li>
          </ul>
        ) : (
          <ul>
            <li>
              <Link to="/oauth-login" role="button">
                Login
              </Link>
            </li>
          </ul>
        )}
      </nav>
    </header>
  );
};

export default Header;
