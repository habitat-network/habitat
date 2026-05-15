import { useMutation } from "@tanstack/react-query";
import { Actor, AuthManager, procedure } from "internal";
import {
  Button,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  Textarea,
  RadioGroup,
  RadioGroupItem,
  Label,
} from "internal/components/ui";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { UserCombobox } from "internal";
import { useRouter } from "@tanstack/react-router";

type Visibility = "followers" | "specific";

interface FormData {
  content: string;
  visibility: Visibility;
}

interface NewPostButtonProps {
  authManager: AuthManager;
  _isOnboarded: boolean; // TODO: add this later when its not a toy demo and we will actually persist your data
}

export function NewPostButton({ authManager }: NewPostButtonProps) {
  const router = useRouter();
  const [modalOpen, setModalOpen] = useState(false);
  const [specificUsers, setSpecificUsers] = useState<Actor[]>([]);
  const [postError, setPostError] = useState<string | null>(null);
  const { handleSubmit, register, watch, reset, setValue } = useForm<FormData>({
    defaultValues: { visibility: "specific" },
  });
  const visibility = watch("visibility");

  const closeModal = () => {
    setModalOpen(false);
    setSpecificUsers([]);
    setPostError(null);
    reset();
  };

  const { mutate: createPost, isPending: createPostIsPending } = useMutation({
    onSuccess: () => {
      router.invalidate();
    },
    mutationFn: async (formData: FormData) => {
      const did = authManager.getAuthInfo()!.did;
      const record = {
        $type: "app.bsky.feed.post",
        text: formData.content,
        createdAt: new Date().toISOString(),
      };

      if (formData.visibility === "followers") {
        await procedure(
          "network.habitat.repo.putRecord",
          {
            repo: did,
            collection: "app.bsky.feed.post",
            record,
            grantees: [
              {
                $type: "network.habitat.grantee#clique",
                clique: `habitat://${did}/network.habitat.clique/followers`,
              },
            ],
          },
          { authManager },
        );
      } else {
        const dids = await Promise.all(
          specificUsers.map(async ({ handle }) => {
            if (!handle) return "";
            const params = new URLSearchParams({ handle });
            const res = await fetch(
              `https://public.api.bsky.app/xrpc/com.atproto.identity.resolveHandle?${params.toString()}`,
            );
            const { did } = await res.json();
            return did;
          }),
        );
        const cliqueData = await procedure(
          "network.habitat.repo.putRecord",
          {
            repo: did,
            collection: "network.habitat.clique",
            record,
            grantees: dids.map((did) => ({
              $type: "network.habitat.grantee#didGrantee",
              did,
            })),
          },
          { authManager },
        );
        const cliqueUri = cliqueData.uri;

        await procedure(
          "network.habitat.repo.putRecord",
          {
            repo: did,
            collection: "app.bsky.feed.post",
            record,
            grantees: [
              {
                $type: "network.habitat.grantee#clique",
                clique: cliqueUri,
              },
            ],
          },
          { authManager },
        );
      }
    },
  });

  return (
    // For demo / toy purposes, allow people to make private posts without onboarding.
    <>
      <Button onClick={() => setModalOpen(true)}>New Post</Button>
      <Dialog open={modalOpen} onOpenChange={setModalOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New post</DialogTitle>
          </DialogHeader>
          {/*!isOnboarded && (
            <p>
              To make private posts, you need to be onboarded to habitat.{" "}
              <a href="https://habitat.network/habitat/#/onboard">
                --&gt; Onboard
              </a>
            </p>
          )*/}
          {
            /*!!isOnboarded &&*/ <form
              onSubmit={handleSubmit(async (data) => {
                setPostError(null);
                createPost(data, {
                  onError: (error) => setPostError(error.message),
                  onSuccess: () => closeModal(),
                });
              })}
              className="space-y-4"
            >
              <Textarea
                placeholder="What's on your mind?"
                {...register("content")}
              />
              <RadioGroup
                value={visibility}
                onValueChange={(value) =>
                  setValue("visibility", value as Visibility)
                }
              >
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="followers" id="followers" />
                  <Label htmlFor="followers">Bluesky followers only</Label>
                </div>
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="specific" id="specific" />
                  <Label htmlFor="specific">Specific users</Label>
                </div>
              </RadioGroup>
              {visibility === "specific" && (
                <UserCombobox
                  value={specificUsers}
                  onValueChange={setSpecificUsers}
                />
              )}
              {postError && (
                <p className="text-destructive text-sm">{postError}</p>
              )}
              <Button type="submit" disabled={createPostIsPending}>
                {createPostIsPending ? "Posting..." : "Post"}
              </Button>
            </form>
          }
        </DialogContent>
      </Dialog>
    </>
  );
}
