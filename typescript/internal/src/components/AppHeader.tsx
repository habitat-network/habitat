import { Actor } from "@/types/Actor";
import { UserAvatar } from "./UserAvatar";
import { HabitatLogo } from "./HabitatLogo";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "./ui/dropdown-menu";
import { Item, ItemContent, ItemTitle, ItemDescription } from "./ui/item";
import { LogOut } from "lucide-react";

interface AppHeaderProps {
  actor?: Actor;
  onSignOut?: () => void;
}

export const AppHeader = ({ actor, onSignOut }: AppHeaderProps) => {
  return (
    <nav className="flex items-center justify-between w-full p-4">
      <HabitatLogo />
      {actor && (
        <DropdownMenu>
          <DropdownMenuTrigger>
            <UserAvatar actor={actor} size="lg" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <Item>
              <ItemContent>
                <ItemTitle>{actor.displayName}</ItemTitle>
                <ItemDescription>@{actor.handle}</ItemDescription>
              </ItemContent>
            </Item>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onSignOut}>
              <LogOut />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </nav>
  );
};
