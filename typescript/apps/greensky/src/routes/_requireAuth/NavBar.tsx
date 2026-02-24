import { type ReactNode } from "react";
import { type AuthManager } from "internal/authManager.js";
import { type Profile } from "../../habitatApi";
import { NewPostButton } from "./NewPostButton";

interface NavBarProps {
  left: ReactNode;
  authManager: AuthManager;
  myProfile: Profile | undefined;
}

export function NavBar({ left, authManager, myProfile }: NavBarProps) {
  return (
    <nav>
      <ul>
        {left}
      </ul>
      <ul>
        <li>
          <span>@{myProfile?.handle}</span>
        </li>
        <li>
          <NewPostButton authManager={authManager} />
        </li>
        <li>
          <button className="secondary" onClick={authManager.logout}>Logout</button>
        </li>
      </ul>
    </nav>
  );
}
