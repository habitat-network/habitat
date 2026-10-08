import { Link, useParams } from "@tanstack/react-router";
import { Actor, UserAvatar, type AuthManager } from "internal";
import { Button } from "internal/components/ui";
import { OrgSwitcher } from "@/components/OrgSwitcher";

interface HeaderProps {
  profile?: Actor;
  authManager: AuthManager;
  onLogout: () => void;
}

const SettingsLink = () => {
  const { org } = useParams({ strict: false });
  if (!org) return null;
  return (
    <li>
      <Button
        variant="link"
        render={<Link to="/orgs/$org/settings" params={{ org }} />}
      >
        Settings
      </Button>
    </li>
  );
};

// SpacesLink goes to the current org's spaces when the route names an org,
// and to the all-spaces page otherwise.
const SpacesLink = () => {
  const { org } = useParams({ strict: false });
  return (
    <li>
      <Button
        variant="link"
        render={
          org ? (
            <Link to="/spaces/$spaceOwner" params={{ spaceOwner: org }} />
          ) : (
            <Link to="/spaces" />
          )
        }
      >
        Spaces
      </Button>
    </li>
  );
};

const Header = ({ profile, authManager, onLogout }: HeaderProps) => {
  return (
    <header className="w-full">
      <nav className="flex justify-between py-4 px-6 items-center border-b">
        <ul className="flex items-center gap-4">
          <li>
            <Link to="/">🌱 habitat</Link>
          </li>
          {profile && (
            <>
              <li>
                <OrgSwitcher authManager={authManager} />
              </li>
              <SpacesLink />
              <SettingsLink />
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
