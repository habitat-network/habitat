import { Link } from '@tanstack/react-router';
import { useAuth } from './authContext';

function formatHandle(handle: string | null) {
  if (!handle) return '';
  const parts = handle.split('.');
  if (parts.length > 1) {
    return `${parts[0]}@${parts.slice(1).join('.')}`;
  }
  return handle;
}

interface HeaderProps {
  isAuthenticated: boolean
  handle: string | undefined
  onLogout: () => void
}

const Header = ({ isAuthenticated: isOauthAuthenticated, handle: oauthHandle, onLogout: onOauthLogout }: HeaderProps) => {
  const { isAuthenticated, handle, logout } = useAuth();
  return (
    <header >
      <nav>
        <ul>
          <li><Link to="/">🌱 Habitat</Link></li>
        </ul>
        {isOauthAuthenticated ? (
          <ul>
            <li>
              <button onClick={onOauthLogout}>
                OAuth Logout {oauthHandle && `(${formatHandle(oauthHandle)})`}
              </button>
            </li>
          </ul>
        ) : (
          <ul>
            <li><Link to="/oauth-login"><button>OAuth Login</button></Link></li>
          </ul>
        )}
        {isAuthenticated && (
          <ul >
            <li>
              {handle && formatHandle(handle)}
            </li>
            <li><button onClick={logout}>
              Logout
            </button></li>
          </ul>
        )}
      </nav>
    </header>
  );
};

export default Header;
