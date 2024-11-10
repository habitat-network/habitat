import React from 'react';
import styles from './header.module.css';
import { useAuth } from './authContext';

interface HeaderProps {
  isAuthenticated: boolean;
  handle: string | null;
  logout: () => void;
}

function formatHandle(handle: string | null) {
  if (!handle) return '';
  const parts = handle.split('.');
  if (parts.length > 1) {
    return `${parts[0]}@${parts.slice(1).join('.')}`;
  }
  return handle;
}

const Header: React.FC<HeaderProps> = ({ isAuthenticated, handle, logout }) => {
  return (
    <header className={styles.header}>
      <div className={styles.logo}>
        <a href="/" className={styles.logoLink}>
          <span className={styles.logo}>🌱</span>
          <span className={styles.logo}> Habitat</span>
        </a>
      </div>
      {isAuthenticated && (
      <div className={styles.userInfo}>
        {handle && <span className={styles.handle}>{formatHandle(handle)}</span>}
        <button className={styles.logoutButton} onClick={logout}>
          Logout
          </button>
        </div>
      )}
    </header>
  );
};

export default Header;
