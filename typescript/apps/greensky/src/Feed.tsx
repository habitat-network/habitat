import { Link } from "@tanstack/react-router";
import React from "react";
import type { PostVisibility } from "./habitatApi";

export interface FeedEntry {
  uri: string;
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
  grantees?: { avatar?: string; handle: string }[];
}

function bskyUrl(uri: string, handle: string): string {
  const rkey = uri.split("/").pop();
  return `https://bsky.app/profile/${handle}/post/${rkey}`;
}

export function Feed({
  entries,
  showPrivatePermalink = true,
  renderPostActions,
}: {
  entries: FeedEntry[];
  showPrivatePermalink?: boolean;
  renderPostActions?: (entry: FeedEntry) => React.ReactNode;
}) {
  const sorted = [...entries].sort((a, b) => {
    if (!a.createdAt && !b.createdAt) return 0;
    if (!a.createdAt) return 1;
    if (!b.createdAt) return -1;
    return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
  });

  return (
    <>
      {sorted.map((entry) => (
        <article
          key={entry.uri}
          style={{
            outline:
              entry.kind === "specific-users"
                ? "3px solid #E99FED"
                : entry.kind === "followers-only"
                  ? "3px solid #2A7047"
                  : "3px solid #92C0D1",
            position: "relative",
          }}
        >
          {entry.kind === "public" && entry.author?.handle && (
            <a
              href={bskyUrl(entry.uri, entry.author.handle)}
              target="_blank"
              rel="noopener noreferrer"
              style={{
                position: "absolute",
                top: 8,
                right: 8,
                fontSize: "0.75em",
                textDecoration: "none",
              }}
            >
              ↗🦋
            </a>
          )}
          {showPrivatePermalink && entry.kind !== "public" && entry.author?.handle && (
            <Link
              to={"/$handle/p/$rkey" as any}
              params={{ handle: entry.author.handle, rkey: entry.uri.split("/").pop()! } as any}
              style={{
                position: "absolute",
                top: 8,
                right: 8,
                fontSize: "0.75em",
                textDecoration: "none",
              }}
            >
              ↗🌱
            </Link>
          )}
          {entry.grantees && entry.grantees.length > 0 && (
            <div
              style={{
                position: "absolute",
                top: 8,
                right: showPrivatePermalink && entry.kind !== "public" ? 48 : 8,
                display: "flex",
              }}
            >
              {entry.grantees.map((grantee, i) => (
                <a
                  key={grantee.handle}
                  href={`https://bsky.app/profile/${grantee.handle}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{ marginLeft: i === 0 ? 0 : -6, display: "block" }}
                >
                  {grantee.avatar ? (
                    <img
                      src={grantee.avatar}
                      width={24}
                      height={24}
                      style={{
                        borderRadius: "50%",
                        border: "2px solid white",
                        display: "block",
                      }}
                    />
                  ) : (
                    <div
                      style={{
                        width: 24,
                        height: 24,
                        borderRadius: "50%",
                        border: "2px solid white",
                        background: "#ccc",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        fontSize: 10,
                        color: "#555",
                        boxSizing: "border-box",
                      }}
                    >
                      {grantee.handle[0]?.toUpperCase()}
                    </div>
                  )}
                </a>
              ))}
            </div>
          )}
          <header>
            <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
              {entry.kind === "public"
                ? "Public"
                : entry.kind === "followers-only"
                  ? "Followers only"
                  : "Specific users only"}
            </div>
            {entry.repostedByHandle !== undefined && (
              <div
                style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}
              >
                ↻ reposted by @{entry.repostedByHandle}
              </div>
            )}
            {entry.replyToHandle !== undefined && (
              <div
                style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}
              >
                {entry.replyToHandle !== null
                  ? `← reply to @${entry.replyToHandle}`
                  : "← reply"}
              </div>
            )}
            {entry.author &&
              (entry.author.handle ? (
                <Link
                  to={"/handle/$handle" as any}
                  params={{ handle: entry.author.handle } as any}
                >
                  {entry.author.avatar && (
                    <img
                      src={entry.author.avatar}
                      width={24}
                      height={24}
                      style={{ marginRight: 8 }}
                    />
                  )}
                  {entry.author.displayName ?? entry.author.handle}
                </Link>
              ) : (
                <span>
                  {entry.author.avatar && (
                    <img
                      src={entry.author.avatar}
                      width={24}
                      height={24}
                      style={{ marginRight: 8 }}
                    />
                  )}
                  {entry.author.displayName}
                </span>
              ))}
          </header>
          {entry.text}
          {renderPostActions?.(entry)}
        </article>
      ))}
    </>
  );
}
