import React from "react";
import { Actor } from "@/types/Actor";
import { UserAvatar } from "./UserAvatar";
import {
  Item,
  ItemMedia,
  ItemContent,
  ItemTitle,
  ItemDescription,
  ItemActions,
} from "./ui/item";

export interface UserItemProps {
  actor: Actor;
  avatarLink?: boolean;
  size?: "default" | "sm" | "lg";
  className?: string;
  actions?: React.ReactNode;
}

export function UserItem({
  actor,
  avatarLink = false,
  size = "default",
  className,
  actions,
}: UserItemProps) {
  const { displayName, handle } = actor;
  return (
    <Item className={className}>
      <ItemMedia variant="image">
        <UserAvatar actor={actor} link={avatarLink} size={size} />
      </ItemMedia>
      <ItemContent>
        {displayName && <ItemTitle>{displayName}</ItemTitle>}
        {handle && <ItemDescription>@{handle}</ItemDescription>}
        {!displayName && !handle && (
          <ItemDescription>Unknown User</ItemDescription>
        )}
      </ItemContent>
      {actions && <ItemActions>{actions}</ItemActions>}
    </Item>
  );
}
