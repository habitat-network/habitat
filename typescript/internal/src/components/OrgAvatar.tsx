import { Avatar, AvatarImage, AvatarFallback } from "./ui/avatar";
import BoringAvatar from "boring-avatars";

export interface OrgAvatarProps {
  did: string;
  name?: string;
  avatarUrl?: string;
  size?: "default" | "sm" | "lg";
  className?: string;
}

export function OrgAvatar({
  did,
  name,
  avatarUrl,
  size = "default",
  className,
}: OrgAvatarProps) {
  return (
    <Avatar size={size} className={className}>
      <AvatarImage src={avatarUrl} alt={name || did} />
      <AvatarFallback>
        <BoringAvatar name={did} variant="marble" />
      </AvatarFallback>
    </Avatar>
  );
}
