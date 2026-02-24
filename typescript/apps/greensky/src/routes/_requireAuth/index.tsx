import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { getPrivatePosts, getProfile, PrivatePost, type Profile } from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";

type Visibility = "public" | "followers" | "specific";

interface FormData {
  content: string;
  visibility: Visibility;
}

interface BskyAuthor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface BskyPost {
  uri: string;
  author?: BskyAuthor;
  record: {
    text: string;
    createdAt?: string;
  };
}

interface BskyFeedItem {
  post: BskyPost;
  reply?: {
    parent: {
      uri: string;
      author?: BskyAuthor;
    };
  };
  reason?: {
    $type: string;
    by: BskyAuthor;
  };
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const bskyItems: BskyFeedItem[] = await getBskyFeed(context.authManager);
    const privatePosts = await getPrivatePosts(context.authManager);

    const privatePostToFeedEntry = async (post: PrivatePost): Promise<FeedEntry> => {
      const did = post.uri.split("/")[2];
      const privateAuthor: Profile | undefined = did
        ? await getProfile(context.authManager, did)
        : undefined;

      return {
        uri: post.uri,
        text: post.value.text,
        createdAt: post.value.createdAt,
        kind: "private",
        author: privateAuthor,
        replyToHandle: post.value.reply !== undefined ? null : undefined,
      }
    }
    const privateEntries = await Promise.all(privatePosts.map(privatePostToFeedEntry))

    const entries: FeedEntry[] = [
      ...privateEntries,
      ...bskyItems.map(({ post, reply, reason }): FeedEntry => ({
        uri: post.uri,
        text: post.record.text,
        createdAt: post.record.createdAt,
        kind: "public",
        author: post.author,
        replyToHandle: reply !== undefined
          ? (reply.parent.author?.handle ?? null)
          : undefined,
        repostedByHandle:
          reason?.$type === "app.bsky.feed.defs#reasonRepost"
            ? reason.by.handle
            : undefined,
      })),
    ];

    return entries;
  },
  component() {
    const { authManager } = Route.useRouteContext();
    const entries = Route.useLoaderData();
    const [modalOpen, setModalOpen] = useState(false);
    const [specificUsers, setSpecificUsers] = useState<string[]>([]);
    const [userQuery, setUserQuery] = useState("");
    const [suggestions, setSuggestions] = useState<{ handle: string; displayName?: string; avatar?: string }[]>([]);
    const [postError, setPostError] = useState<string | null>(null);

    useEffect(() => {
      if (!userQuery.trim()) { setSuggestions([]); return; }
      const timer = setTimeout(async () => {
        const headers = new Headers();
        headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
        const params = new URLSearchParams({ q: userQuery, limit: "8" });
        const res = await (authManager as any).fetch(
          `/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
          "GET", null, headers,
        );
        const data: { actors: { handle: string; displayName?: string; avatar?: string }[] } = await res.json();
        setSuggestions(data.actors ?? []);
      }, 250);
      return () => clearTimeout(timer);
    }, [userQuery]);
    const { handleSubmit, register, watch, reset } = useForm<FormData>({
      defaultValues: { visibility: "public" },
    });
    const visibility = watch("visibility");

    const addUser = (handle: string) => {
      const val = handle.trim().replace(/^@/, "");
      if (val && !specificUsers.includes(val)) {
        setSpecificUsers((prev) => [...prev, val]);
      }
      setUserQuery("");
      setSuggestions([]);
    };

    const closeModal = () => {
      setModalOpen(false);
      setSpecificUsers([]);
      setUserQuery("");
      setSuggestions([]);
      setPostError(null);
      reset();
    };

    const { mutate: createPost, isPending: createPostIsPending } = useMutation({
      mutationFn: async (formData: FormData) => {
        const record = {
          $type: "app.bsky.feed.post",
          text: formData.content,
          createdAt: new Date().toISOString(),
        };

        async function checkResponse(res: Response) {
          if (!res.ok) {
            let message = `Request failed: ${res.status} ${res.statusText}`;
            try {
              const body = await res.json();
              if (body.message) message = body.message;
              else if (body.error) message = body.error;
            } catch { }
            throw new Error(message);
          }
        }

        if (formData.visibility === "public") {
          const res = await (authManager as any).fetch(
            "/xrpc/com.atproto.repo.createRecord",
            "POST",
            JSON.stringify({
              repo: (authManager as any).did ?? (authManager as any).handle,
              collection: "app.bsky.feed.post",
              record,
            }),
          );
          await checkResponse(res);
        } else if (formData.visibility === "followers") {
          const did = (authManager as any).did;
          const res = await (authManager as any).fetch(
            "/xrpc/network.habitat.putRecord",
            "POST",
            JSON.stringify({
              repo: (authManager as any).handle,
              collection: "app.bsky.feed.post",
              record,
              grantees: [{ $type: "network.habitat.grantee#cliqueRef", uri: `habitat://${did}/network.habitat.clique/followers` }],
            }),
          );
          await checkResponse(res);
        } else {
          // Resolve each handle to a DID
          const dids = await Promise.all(
            specificUsers.map(async (handle) => {
              const res = await (authManager as any).fetch(
                `/xrpc/com.atproto.identity.resolveHandle?handle=${encodeURIComponent(handle)}`,
                "GET",
              );
              await checkResponse(res);
              const resolved: { did: string } = await res.json();
              return resolved.did;
            }),
          );
          const res = await (authManager as any).fetch(
            "/xrpc/network.habitat.putRecord",
            "POST",
            JSON.stringify({
              repo: (authManager as any).handle,
              collection: "app.bsky.feed.post",
              record,
              grantees: dids.map((did) => ({ $type: "network.habitat.grantee#didGrantee", did })),
            }),
          );
          await checkResponse(res);
        }
      },
    });
    return (
      <>
        <nav>
          <ul>
            <li>
              <h2>Greensky</h2>
            </li>
          </ul>
          <ul>
            <li>
              <span>@{(authManager as any).handle}</span>
            </li>
            <li>
              <button onClick={() => setModalOpen(true)}>New Post</button>
            </li>
          </ul>
        </nav>
        <Feed entries={entries} />
        <dialog open={modalOpen}>
          <article>
            <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <h1 style={{ margin: 0 }}>New post</h1>
              <button
                onClick={closeModal}
                aria-label="Close"
                style={{ background: "none", border: "none", fontSize: "1.2rem", cursor: "pointer", padding: 0, lineHeight: 1, color: 'blue' }}
              >
                ✕
              </button>
            </header>
            <form
              onSubmit={handleSubmit(async (data) => {
                setPostError(null);
                createPost(data, {
                  onError: (error) => {
                    setPostError(error.message);
                  },
                  onSuccess: () => {
                    closeModal();
                  },
                });
              })}
            >
              <textarea
                placeholder="What's on your mind?"
                {...register("content")}
              />
              <fieldset>
                <label>
                  <input type="radio" value="public" {...register("visibility")} />
                  Public
                </label>
                <label>
                  <input type="radio" value="followers" {...register("visibility")} />
                  Followers only
                </label>
                <label>
                  <input type="radio" value="specific" {...register("visibility")} />
                  Specific users
                </label>
              </fieldset>
              {visibility === "specific" && (
                <div>
                  <div style={{ position: "relative", marginBottom: "0.5rem" }}>
                    <input
                      type="text"
                      placeholder="Search by handle…"
                      value={userQuery}
                      onChange={(e) => setUserQuery(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          if (suggestions.length > 0) addUser(suggestions[0].handle);
                          else if (userQuery.trim()) addUser(userQuery);
                        }
                      }}
                      style={{ width: "100%" }}
                    />
                    {suggestions.length > 0 && (
                      <ul style={{
                        position: "absolute", top: "100%", left: 0, right: 0, zIndex: 10,
                        margin: 0, padding: 0, listStyle: "none",
                        border: "1px solid ButtonBorder", borderRadius: "4px",
                        backgroundColor: "Canvas", boxShadow: "0 4px 12px rgba(0,0,0,0.1)",
                      }}>
                        {suggestions.map((actor) => (
                          <li
                            key={actor.handle}
                            onClick={() => addUser(actor.handle)}
                            style={{
                              display: "flex", alignItems: "center", gap: "0.5rem",
                              padding: "0.4rem 0.6rem", cursor: "pointer",
                            }}
                            onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = "Highlight")}
                            onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "")}
                          >
                            {actor.avatar && <img src={actor.avatar} width={24} height={24} style={{ borderRadius: "50%" }} />}
                            <span>
                              {actor.displayName && <strong style={{ marginRight: "0.25rem" }}>{actor.displayName}</strong>}
                              <span style={{ color: "GrayText", fontSize: "0.85em" }}>@{actor.handle}</span>
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                  {specificUsers.length > 0 && (
                    <div style={{ display: "flex", flexWrap: "wrap", gap: "0.375rem" }}>
                      {specificUsers.map((u) => (
                        <span
                          key={u}
                          style={{
                            display: "inline-flex", alignItems: "center", gap: "0.25rem",
                            padding: "0.125rem 0.5rem", borderRadius: "999px",
                            border: "1px solid currentColor", fontSize: "0.85rem",
                          }}
                        >
                          @{u}
                          <button
                            type="button"
                            onClick={() => setSpecificUsers((prev) => prev.filter((x) => x !== u))}
                            style={{ background: "none", border: "none", cursor: "pointer", padding: 0, lineHeight: 1 }}
                            aria-label={`Remove ${u}`}
                          >
                            ✕
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              )}
              {postError && (
                <p style={{ color: "red", margin: "0.5rem 0" }}>{postError}</p>
              )}
              <button type="submit" aria-busy={createPostIsPending}>
                Post
              </button>
            </form>
          </article>
        </dialog>
      </>
    );
  },
});

async function getBskyFeed(authManager: AuthManager): Promise<BskyFeedItem[]> {
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const params = new URLSearchParams();
  params.append(
    "feed",
    "at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.generator/whats-hot",
  );
  params.append("limit", "10");
  const feedResponse = await authManager.fetch(
    `/xrpc/app.bsky.feed.getFeed?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  const feedData: { feed: BskyFeedItem[] } = await feedResponse.json();
  return feedData.feed;
}
