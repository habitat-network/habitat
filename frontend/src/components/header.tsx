import { Link } from "@tanstack/react-router";
import { Actor, UserAvatar } from "internal";
import { Button } from "internal/components/ui";

interface HeaderProps {
  profile?: Actor;
  onLogout: () => void;
}

const Header = ({ profile, onLogout }: HeaderProps) => {
  return (
    <header className="w-full">
      <nav className="flex justify-between py-4 px-6 items-center border-b">
        <ul>
          <li>
            <Link to="/">🌱 habitat</Link>
          </li>
        </ul>
        {profile ? (
          <ul className="flex items-center gap-2">
            <Button variant="ghost" render={<Link to="/devtools" />}>
              Devtools
            </Button>
            <UserAvatar actor={profile} />
            <li>
              <Button onClick={onLogout}>Logout</Button>
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
