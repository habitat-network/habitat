import { UserAvatar } from "./UserAvatar";

export interface UserItemProps {
  src?: string;
  displayName?: string;
  handle?: string;
  avatarLink?: boolean;
  size?: "default" | "sm" | "lg";
  className?: string;
}

export function UserItem({
  src,
  displayName,
  handle,
  avatarLink = false,
  size = "default",
  className,
}: UserItemProps) {
  return (
    <div className={`flex items-center gap-3 ${className || ""}`}>
      <UserAvatar
        src={src}
        displayName={displayName}
        handle={handle}
        link={avatarLink}
        size={size}
      />
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
