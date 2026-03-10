import { Actor } from "@/types/Actor";
import { UserAvatar } from "./UserAvatar";

export interface UserItemProps {
  actor: Actor;
  avatarLink?: boolean;
  size?: "default" | "sm" | "lg";
  className?: string;
}

export function UserItem({
  actor,
  avatarLink = false,
  size = "default",
  className,
}: UserItemProps) {
  const { displayName, handle } = actor;
  return (
    <div className={`flex items-center gap-3 ${className || ""}`}>
      <UserAvatar actor={actor} link={avatarLink} size={size} />
      <div className="flex flex-col min-w-0 flex-1">
        {displayName && (
          <div className="font-semibold truncate">{displayName}</div>
        )}
        {handle && (
          <div className="text-sm text-muted-foreground truncate">
            @{handle}
          </div>
        )}
        {!displayName && !handle && (
          <div className="text-sm text-muted-foreground">Unknown User</div>
        )}
      </div>
    </div>
  );
}
