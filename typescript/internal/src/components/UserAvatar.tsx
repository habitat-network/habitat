import { Actor } from "@/types/Actor";
import { Avatar, AvatarImage, AvatarFallback } from "./ui/avatar";
import BoringAvatar from 'boring-avatars';

export interface UserAvatarProps {
  actor: Actor;
  size?: "default" | "sm" | "lg";
  className?: string;
  link?: boolean;
}

export function UserAvatar({
  actor,
  size = "default",
  className,
  link = false,
}: UserAvatarProps) {
  const { displayName, handle, did } = actor;
  // Generate alt text from displayName or handle
  const alt = displayName || (handle ? `@${handle}` : "User");

  const avatar = (
    <Avatar size={size} className={className}>
      <AvatarImage src={actor.avatar} alt={alt} />
      <AvatarFallback>
        <BoringAvatar name={did} variant="beam" />
      </AvatarFallback>
    </Avatar>
  );

  // If link is true and handle exists, wrap in a link
  if (link && handle) {
    return (
      <a
        href={`https://bsky.app/profile/${handle}`}
        target="_blank"
        rel="noopener noreferrer"
        title={`View @${handle} on Bluesky`}
      >
        {avatar}
      </a>
    );
  }

  return avatar;
}
