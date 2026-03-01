import { type ReactNode } from "react";
import { type AuthManager } from "internal/authManager.js";
import { type Profile } from "../habitatApi";
import { NewPostButton } from "./NewPostButton";
import { Link } from "@tanstack/react-router";

interface NavBarProps {
  left: ReactNode;
  authManager: AuthManager;
  myProfile: Profile | undefined;
  isOnboarded: boolean;
}

export function NavBar({
  left,
  authManager,
  myProfile,
  isOnboarded,
}: NavBarProps) {
  return (
    <div>
      <nav>
        <ul>{left}</ul>
        <ul>
          <li>
            {myProfile?.handle && <span><Link to={`/handle/${myProfile?.handle}`}>@{myProfile?.handle}</Link></span>}
          </li>
          <li>
            <NewPostButton authManager={authManager} _isOnboarded={isOnboarded} />
          </li>
          <li>
            <button className="secondary" onClick={authManager.logout}>
              Logout
            </button>
          </li>
        </ul>
      </nav>
      <p>✨ This is an experimental demo, to show an app built on top of our implementation of permissioned data for ATProtocol. Any posts you create through this app are not guaranteed to be persisted and will likely be deleted as we continue to iterate. Click on a user to see a feed of all their Bluesky posts, private + public. Thanks for stopping by! ✨</p>
    </div>

  );
}
