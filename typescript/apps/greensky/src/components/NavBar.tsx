import { type ReactNode } from "react";
import { type AuthManager } from "internal";
import { Button, Separator } from "internal/components/ui";
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
    <div className="container mx-auto px-4 flex flex-col items-center">
      <nav className="flex flex-wrap items-center py-4 w-full gap-y-2">
        <ul className="flex-1 flex items-center gap-4 list-none m-0 p-0 text-[green]">{left}</ul>
        <a href="https://habitat.network" style={{ color: "green" }} className="text-sm">
          by habitat 🌱
        </a>
        <div className="flex-1 flex items-center gap-4 justify-end">
          {myProfile?.handle && (
            <Link
              to="/handle/$handle"
              params={{ handle: myProfile.handle }}
              className="hover:underline sm:inline"
            >
              @{myProfile.handle}
            </Link>
          )}
          <NewPostButton authManager={authManager} _isOnboarded={isOnboarded} />
          <Button variant="secondary" onClick={authManager.logout}>
            Logout
          </Button>
        </div>
      </nav>
      <Separator />
      <p className="m-4 text-sm text-muted-foreground prose max-w-none w-full">
        ✨ This is an experimental demo, to show what it might feel like to use an app with public and permissioned data
        on ATProtocol and proof out our implementation. Any posts you create through greensky are not guaranteed to persist and will likely be
        deleted as we iterate. Click on a user to see a feed of all their Bluesky posts, private + public. Thanks for stopping by! ✨
      </p>
    </div>
  );
}
