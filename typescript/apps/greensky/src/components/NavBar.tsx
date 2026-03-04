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
      <nav className="flex items-center justify-between py-4 w-full">
        <div className="flex items-center gap-4">{left}</div>
        <div className="flex items-center gap-4">
          {myProfile?.handle && (
            <Link
              to="/handle/$handle"
              params={{ handle: myProfile.handle }}
              className="hover:underline"
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
      <p className="m-4 text-sm text-muted-foreground prose">
        ✨ This is an experimental demo, to show an app built on top of our
        implementation of permissioned data for ATProtocol. Any posts you create
        through this app are not guaranteed to be persisted and will likely be
        deleted as we continue to iterate. Click on a user to see a feed of all
        their Bluesky posts, private + public. Thanks for stopping by! ✨
      </p>
    </div>
  );
}
