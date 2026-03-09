import { Link } from "@tanstack/react-router";
import { UserAvatar } from "internal";
import { ProfileViewDetailed } from "@atproto/api/dist/client/types/app/bsky/actor/defs";


interface HeaderProps {
  profile?: ProfileViewDetailed;
  onLogout: () => void;
}

const Header = ({ profile, onLogout }: HeaderProps) => {
  return (
    <header>
      <nav>
        <ul>
          <li>
            <Link to="/">🌱 habitat</Link>
          </li>
        </ul>
        {profile ? (
          <ul>
            <UserAvatar
              src={profile.avatar}
              displayName={profile.displayName}
              handle={profile.handle}
            />
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
