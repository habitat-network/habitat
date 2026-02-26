import { Link } from "@tanstack/react-router";

export interface FeedEntry {
  uri: string;
  text: string;
  createdAt?: string;
  kind: "public" | "private";
  author?: {
    handle?: string;
    displayName?: string;
    avatar?: string;
  };
  // undefined = not a reply; null = reply but parent handle unknown; string = reply to this handle
  replyToHandle?: string | null;
  repostedByHandle?: string;
}

function bskyUrl(uri: string, handle: string): string {
  const rkey = uri.split("/").pop();
  return `https://bsky.app/profile/${handle}/post/${rkey}`;
}

export function Feed({ entries }: { entries: FeedEntry[] }) {
  const sorted = [...entries].sort((a, b) => {
    if (!a.createdAt && !b.createdAt) return 0;
    if (!a.createdAt) return 1;
    if (!b.createdAt) return -1;
    return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime();
  });

  return (
    <>
      {sorted.map((entry) => (
        <article
          key={entry.uri}
          style={{
            outline:
              entry.kind === "private" ? "3px solid green" : "3px solid lightblue",
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
          <header>
            {entry.repostedByHandle !== undefined && (
              <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                ↻ reposted by @{entry.repostedByHandle}
              </div>
            )}
            {entry.replyToHandle !== undefined && (
              <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                {entry.replyToHandle !== null
                  ? `← reply to @${entry.replyToHandle}`
                  : "← reply"}
              </div>
            )}
            {entry.author && (
              entry.author.handle ? (
                <Link to={"/handle/$handle" as any} params={{ handle: entry.author.handle } as any}>
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
              )
            )}
          </header>
          {entry.text}
        </article>
      ))}
    </>
  );
}
