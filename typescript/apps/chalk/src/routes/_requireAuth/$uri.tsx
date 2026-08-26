import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { ShareDialog, getProfiles, type Actor } from "internal";
import { toast } from "internal/components/ui";
import { PageHeader } from "@/components/PageHeader";
import { HelpDialog } from "@/components/HelpDialog";
import { useYDoc } from "@/hooks/useYDoc";
import { listDocAccess, revokeDocAccess, shareDoc } from "@/server/functions";
import { useRecentDocsStore } from "@/stores/recentDocs";

export const Route = createFileRoute("/_requireAuth/$uri")({
  component() {
    const { uri } = Route.useParams();
    const ydoc = useYDoc(uri);
    const queryClient = useQueryClient();

    const addRecentDoc = useRecentDocsStore((state) => state.addRecentDoc);
    useEffect(() => addRecentDoc(uri), [uri, addRecentDoc]);

    const accessQueryKey = ["docAccess", uri];
    const { data: grantees = [] } = useQuery({
      queryKey: accessQueryKey,
      // listDocAccess only returns DIDs (what network.habitat.relationship
      // actually stores); resolving those to handles/avatars for display is
      // a separate, client-side lookup against the public directory.
      queryFn: async () => {
        const access = await listDocAccess({ data: { docId: uri } });
        return getProfiles(access.map((a) => a.did));
      },
    });
    const invalidateAccess = () =>
      queryClient.invalidateQueries({ queryKey: accessQueryKey });

    const { mutate: addPermission, isPending: isAddingPermission } =
      useMutation({
        mutationFn: (actors: Actor[]) =>
          Promise.all(
            actors.map((actor) =>
              shareDoc({ data: { docId: uri, subjectDid: actor.did } }),
            ),
          ),
        onSuccess: invalidateAccess,
        onError: (error) => {
          toast.add({
            type: "error",
            title: "Couldn't share doc",
            description: error.message,
          });
        },
      });

    const { mutate: removePermission } = useMutation({
      mutationFn: (actor: Actor) =>
        revokeDocAccess({ data: { docId: uri, subjectDid: actor.did } }),
      onSuccess: invalidateAccess,
      onError: (error) => {
        toast.add({
          type: "error",
          title: "Couldn't remove access",
          description: error.message,
        });
      },
    });

    const editor = useEditor(
      {
        // The doc's real content comes from ydoc over the WebSocket, not
        // server-rendered markup — rendering on the server would touch
        // `document`, which doesn't exist there, and would just be thrown
        // away on hydration anyway.
        immediatelyRender: false,
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
      },
      [ydoc],
    );

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <div className="flex gap-2">
            <ShareDialog
              grantees={grantees}
              isAdding={isAddingPermission}
              onAddPermission={(actors) => addPermission(actors)}
              onRemovePermission={(actor) => removePermission(actor)}
            />
            <HelpDialog />
          </div>
        </PageHeader>
      </div>
    );
  },
  pendingComponent: () => <article>Loading...</article>,
});
