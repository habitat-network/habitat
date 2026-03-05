import { Link } from "@tanstack/react-router";
import React from "react";
import { AuthManager } from "internal";
import { PostReply } from "./components/PostReply";
import {
  Card,
  CardHeader,
  CardContent,
  CardDescription,
  AvatarGroup,
  CardFooter,
  Item,
  ItemMedia,
  ItemContent,
  ItemTitle,
  ItemDescription,
  ItemActions,
  Badge,
} from "internal/components/ui";
import { UserAvatar } from "internal";
import type { PostVisibility } from "./habitatApi";

export interface FeedEntry {
  uri: string;
  cid?: string;
  clique?: string;
  text: string;
  createdAt?: string;
  kind: PostVisibility;
  author?: {
    handle?: string;
    displayName?: string;
    avatar?: string;
  };
  // undefined = not a reply; null = reply but parent handle unknown; string = reply to this handle
  replyToHandle?: string | null;
  repostedByHandle?: string;
  quotedPost?: { bskyUrl: string; authorHandle: string };
  grantees?: { avatar?: string; handle: string }[];
}

function bskyUrl(uri: string, handle: string): string {
  const rkey = uri.split("/").pop();
  return `https://bsky.app/profile/${handle}/post/${rkey}`;
}

export function Feed({
  entries,
  showPrivatePermalink = true,
  authManager,
}: {
  entries: FeedEntry[];
  showPrivatePermalink?: boolean;
  authManager?: AuthManager;
}) {
  // Reverse chronological, with createdAt missing or 0 at the end.
  const sorted = [...entries].sort((a, b) => {
    const aTime = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const bTime = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    const aEmpty = !aTime;
    const bEmpty = !bTime;
    if (aEmpty && bEmpty) return 0;
    if (aEmpty) return -1;
    if (bEmpty) return 1;
    return bTime - aTime;
  });

  return (
    <div className="flex flex-col gap-4 w-full max-w-2xl px-2">
      {sorted.map((entry) => (
        <Card
          key={entry.uri}
          size="sm"
          className={
            entry.kind === "public"
              ? "ring-3 ring-[#92C0D1]"
              : entry.kind === "followers-only"
                ? "ring-3 ring-[#2A7047]"
                : "ring-3 ring-[#FA9EE5]"
          }
        >
          <CardHeader>
            <CardDescription className="flex items-center gap-2 flex-wrap">
              {entry.repostedByHandle && (
                <span className="text-xs">
                  ↻ reposted by @{entry.repostedByHandle}
                </span>
              )}
              {entry.replyToHandle !== undefined && (
                <span className="text-xs">
                  {entry.replyToHandle !== null
                    ? `← reply to @${entry.replyToHandle}`
                    : "← reply"}
                </span>
              )}
              {entry.quotedPost && (
                <span className="text-xs">
                  ↗ quote repost of{" "}
                  <a
                    href={entry.quotedPost.bskyUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    @{entry.quotedPost.authorHandle}
                  </a>
                </span>
              )}
            </CardDescription>
            {entry.author && (
              <Item size="xs" variant="muted" className="py-1.5">
                <ItemContent>
                  <Item
                    size="xs"
                    className="bg-muted hover:!bg-[#F9FFF2]"
                    render={
                      <Link
                        to={"/handle/$handle"}
                        params={{ handle: entry.author.handle ?? "" }}
                        disabled={!entry.author.handle}
                      />
                    }
                  >
                    <ItemMedia>
                      <UserAvatar
                        src={entry.author.avatar}
                        displayName={entry.author.displayName}
                        handle={entry.author.handle}
                      />
                    </ItemMedia>
                    <ItemContent>
                      <ItemTitle>
                        {entry.author.displayName && (
                          <span>{entry.author.displayName}</span>
                        )}
                        {entry.author.handle && (
                          <span className="font-normal text-muted-foreground">
                            @{entry.author.handle}
                          </span>
                        )}
                      </ItemTitle>
                    </ItemContent>
                  </Item>
                </ItemContent>
                <ItemActions>
                  <Badge variant="secondary">
                    {entry.kind === "public"
                      ? "🌍 Public"
                      : entry.kind === "followers-only"
                        ? "🔒 Followers only"
                        : "👥 Specific users"}
                  </Badge>
                  {entry.grantees && entry.grantees.length > 0 && (
                    <AvatarGroup>
                      {entry.grantees.map((grantee) => (
                        <UserAvatar
                          key={grantee.handle}
                          src={grantee.avatar}
                          handle={grantee.handle}
                          size="sm"
                          link
                        />
                      ))}
                    </AvatarGroup>
                  )}
                  {entry.kind === "public" && entry.author?.handle && (
                    <a
                      href={bskyUrl(entry.uri, entry.author.handle)}
                      target="_blank"
                      rel="noopener noreferrer"
                      title="View in Bluesky"
                    >
                      ↗🦋
                    </a>
                  )}
                  {showPrivatePermalink &&
                    entry.kind !== "public" &&
                    entry.author?.handle && (
                      <Link
                        to={"/$handle/p/$rkey"}
                        params={{
                          handle: entry.author.handle,
                          rkey: entry.uri.split("/").pop()!,
                        }}
                        title="Permalink"
                      >
                        ↗🌱
                      </Link>
                    )}
                </ItemActions>
              </Item>
            )}
          </CardHeader>
          <CardContent className="prose">
            <p className="whitespace-pre-wrap">{entry.text}</p>
          </CardContent>
          <CardFooter>
            {authManager && entry.kind !== "public" && (
              <PostReply
                postUri={entry.uri}
                postCid={entry.cid ?? ""}
                postClique={entry.clique}
                authManager={authManager}
              />
            )}
          </CardFooter>
        </Card>
      ))}
    </div>
  );
}
