import { useMutation } from "@tanstack/react-query";
import { AuthManager } from "internal/authManager.js";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { UserSearch } from "./UserSearch";

type Visibility = "public" | "followers" | "specific";

interface FormData {
  content: string;
  visibility: Visibility;
}

interface NewPostButtonProps {
  authManager: AuthManager;
  _isOnboarded: boolean; // TODO: add this later when its not a toy demo and we will actually persist your data
}

export function NewPostButton({
  authManager,
  _isOnboarded,
}: NewPostButtonProps) {
  const [modalOpen, setModalOpen] = useState(false);
  const [specificUsers, setSpecificUsers] = useState<string[]>([]);
  const [postError, setPostError] = useState<string | null>(null);
  const { handleSubmit, register, watch, reset } = useForm<FormData>({
    defaultValues: { visibility: "public" },
  });
  const visibility = watch("visibility");

  const handleAddUser = (handle: string) => {
    if (handle && !specificUsers.includes(handle)) {
      setSpecificUsers((prev) => [...prev, handle]);
    }
  };

  const closeModal = () => {
    setModalOpen(false);
    setSpecificUsers([]);
    setPostError(null);
    reset();
  };

  const { mutate: createPost, isPending: createPostIsPending } = useMutation({
    mutationFn: async (formData: FormData) => {
      const did = authManager.getAuthInfo()!.did;
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
        const res = await authManager.fetch(
          "/xrpc/com.atproto.repo.createRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: "app.bsky.feed.post",
            record,
          }),
        );
        await checkResponse(res);
      } else if (formData.visibility === "followers") {
        const res = await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: "app.bsky.feed.post",
            record,
            grantees: [
              {
                $type: "network.habitat.grantee#cliqueRef",
                uri: `habitat://${did}/network.habitat.clique/followers`,
              },
            ],
          }),
        );
        await checkResponse(res);
      } else {
        const dids = await Promise.all(
          specificUsers.map(async (handle) => {
            const res = await authManager.fetch(
              `/xrpc/com.atproto.identity.resolveHandle?handle=${encodeURIComponent(handle)}`,
              "GET",
            );
            await checkResponse(res);
            const resolved: { did: string } = await res.json();
            return resolved.did;
          }),
        );
        const cliqueRes = await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: "network.habitat.clique",
            record,
            grantees: dids.map((did) => ({
              $type: "network.habitat.grantee#didGrantee",
              did,
            })),
          }),
        );
        await checkResponse(cliqueRes);
        const data = await cliqueRes.json();
        const cliqueUri = data.uri

        const res = await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: "app.bsky.feed.post",
            record,
            grantees: [
              {
                $type: "network.habitat.grantee#cliqueRef",
                uri: cliqueUri,
              },
            ],
          }),
        );
        await checkResponse(res);
      }
    },
  });

  return (
    // For demo / toy purposes, allow people to make private posts without onboarding.
    <>
      <button onClick={() => setModalOpen(true)}>New Post</button>
      <dialog open={modalOpen}>
        <article>
          <header>
            <button onClick={closeModal} aria-label="Close" rel="prev" />
            <p>
              <strong>New post</strong>
            </p>
          </header>
          {/*!isOnboarded && (
            <p>
              To make private posts, you need to be onboarded to habitat.{" "}
              <a href="https://habitat.network/habitat/#/onboard">
                --&gt; Onboard
              </a>
            </p>
          )*/}
          {/*!!isOnboarded &&*/(
            <form
              onSubmit={handleSubmit(async (data) => {
                setPostError(null);
                createPost(data, {
                  onError: (error) => setPostError(error.message),
                  onSuccess: () => closeModal(),
                });
              })}
            >
              <textarea
                placeholder="What's on your mind?"
                {...register("content")}
              />
              <fieldset>
                <label>
                  <input
                    type="radio"
                    value="public"
                    {...register("visibility")}
                  />
                  Public
                </label>
                <label>
                  <input
                    type="radio"
                    value="followers"
                    {...register("visibility")}
                  />
                  Followers only
                </label>
                <label>
                  <input
                    type="radio"
                    value="specific"
                    {...register("visibility")}
                  />
                  Specific users
                </label>
              </fieldset>
              {visibility === "specific" && (
                <UserSearch
                  authManager={authManager}
                  specificUsers={specificUsers}
                  onAddUser={handleAddUser}
                  onRemoveUser={(u) =>
                    setSpecificUsers((prev) => prev.filter((x) => x !== u))
                  }
                />
              )}
              {postError && <p>{postError}</p>}
              <button type="submit" aria-busy={createPostIsPending}>
                Post
              </button>
            </form>
          )}
        </article>
      </dialog>
    </>
  );
}
