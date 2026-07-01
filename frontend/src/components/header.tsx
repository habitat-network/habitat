import { Link } from "@tanstack/react-router";
import { NetworkHabitatOrgGetMetadata } from "api";
import { Actor, UserAvatar } from "internal";
import { Button } from "internal/components/ui";

interface HeaderProps {
  profile?: Actor;
  org?: NetworkHabitatOrgGetMetadata.OutputSchema;
  onLogout: () => void;
}

const Header = ({ profile, org, onLogout }: HeaderProps) => {
  return (
    <header className="w-full">
      <nav className="flex justify-between py-4 px-6 items-center border-b">
        <ul className="flex items-center gap-4">
          <li>
            <Link to="/">🌱 habitat</Link>
          </li>
          {org && (
            <>
              <li>
                <Button variant="link" render={<Link to="/org" />}>
                  {org.name}
                </Button>
              </li>
              <li>
                <Button variant="link" render={<Link to="/spaces" />}>
                  Spaces
                </Button>
              </li>
            </>
          )}
        </ul>
        {profile ? (
          <ul className="flex items-center gap-2">
            {import.meta.env.DEV && (
              <Button variant="ghost" render={<Link to="/devtools" />}>
                Devtools
              </Button>
            )}
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
