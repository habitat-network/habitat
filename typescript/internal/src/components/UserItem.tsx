import React from "react";
import { Actor, actorLabel } from "../types/Actor";
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
  const { handle } = actor;
  const label = actorLabel(actor);
  return (
    <Item className={className}>
      <ItemMedia variant="image">
        <UserAvatar actor={actor} link={avatarLink} size={size} />
      </ItemMedia>
      <ItemContent>
        <ItemTitle>{label}</ItemTitle>
        {/* The handle is the title when there's no display name; don't repeat it. */}
        {handle && label !== handle && (
          <ItemDescription>@{handle}</ItemDescription>
        )}
      </ItemContent>
      {actions && <ItemActions>{actions}</ItemActions>}
    </Item>
  );
}
