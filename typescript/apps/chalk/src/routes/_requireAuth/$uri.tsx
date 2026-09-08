import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import { createFileRoute } from "@tanstack/react-router";
import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  ShareDialog,
  getProfiles,
  type Actor,
  type ShareDialogGrantee,
  type ShareDialogRole,
} from "internal";
import { Button, toast } from "internal/components/ui";
import { PageHeader } from "@/components/PageHeader";
import { HelpDialog } from "@/components/HelpDialog";
import { CommentSidebar } from "@/components/CommentSidebar";
import { CommentMark } from "@/extensions/comment";
import { useYDoc } from "@/hooks/useYDoc";
import { Route as RequireAuthRoute } from "@/routes/_requireAuth";
import {
  getDocInitialState,
  getDocRole,
  listDocAccess,
  revokeDocAccess,
  shareDoc,
} from "@/server/functions";
import { useRecentDocsStore } from "@/stores/recentDocs";

const docRoleQueryOptions = (docId: string) =>
  queryOptions({
    queryKey: ["docRole", docId],
    queryFn: () => getDocRole({ data: { docId } }),
  });

const docInitialStateQueryOptions = (docId: string) =>
  queryOptions({
    queryKey: ["docInitialState", docId],
    queryFn: () => getDocInitialState({ data: { docId } }),
  });

export const Route = createFileRoute("/_requireAuth/$uri")({
  loader: async ({ context, params }) => {
    const [role, initialState] = await Promise.all([
      context.queryClient.ensureQueryData(docRoleQueryOptions(params.uri)),
      context.queryClient.ensureQueryData(
        docInitialStateQueryOptions(params.uri),
      ),
    ]);
    return { role, initialState };
  },
  component() {
    const { uri } = Route.useParams();
    const { did: currentUserDid } = Route.useRouteContext();
    const { role, initialState } = Route.useLoaderData();
    const { currentOrg } = RequireAuthRoute.useLoaderData();
    const ydoc = useYDoc(uri, initialState);
    const queryClient = useQueryClient();

    const addRecentDoc = useRecentDocsStore((state) => state.addRecentDoc);
    useEffect(() => addRecentDoc(uri), [uri, addRecentDoc]);

    const accessQueryKey = ["docAccess", uri];
    const { data: grantees = [] } = useQuery({
      queryKey: accessQueryKey,
      // listDocAccess only returns DIDs and relations (what
      // network.habitat.relationship actually stores); resolving DIDs to
      // handles/avatars for display is a separate, client-side lookup
      // against the public directory.
      queryFn: async (): Promise<ShareDialogGrantee[]> => {
        const access = await listDocAccess({ data: { docId: uri } });
        const profiles = await getProfiles(access.map((a) => a.did));
        const relationByDid = new Map(
          access.map((a) => [a.did, a.relation] as const),
        );
        return profiles.map((profile) => ({
          ...profile,
          relation: relationByDid.get(profile.did),
        }));
      },
    });
    const invalidateAccess = () =>
      queryClient.invalidateQueries({ queryKey: accessQueryKey });

    const { mutate: addPermission, isPending: isAddingPermission } =
      useMutation({
        mutationFn: ({
          actors,
          role,
        }: {
          actors: Actor[];
          role: ShareDialogRole;
        }) =>
          Promise.all(
            actors.map((actor) =>
              shareDoc({ data: { docId: uri, subjectDid: actor.did, role } }),
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

    // sidebarOpen tracks the comments panel's own visibility, separate
    // from activeThreadId: closing the sidebar shouldn't forget which
    // thread was selected, and clicking a thread mark should reopen it.
    const [sidebarOpen, setSidebarOpen] = useState(false);
    const [activeThreadId, setActiveThreadId] = useState<string | null>(null);
    const [pendingThread, setPendingThread] = useState<{
      threadId: string;
      quotedText: string;
    } | null>(null);

    const editor = useEditor(
      {
        // The doc's real content comes from ydoc over the WebSocket, not
        // server-rendered markup — rendering on the server would touch
        // `document`, which doesn't exist there, and would just be thrown
        // away on hydration anyway.
        immediatelyRender: false,
        // Client-side only: keeps a viewer's editor read-only. The actual
        // access gate is the WS route's reader check (ws.$docId.ts) — this
        // doesn't stop write attempts made outside the UI.
        editable: role === "editor",
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
          CommentMark,
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
        // Clicking (or moving the cursor) into a commented range opens the
        // sidebar on that thread, the same way clicking a comment bubble
        // does in most doc editors. A selection spanning more than one
        // marked range picks the first thread found at its start.
        onSelectionUpdate({ editor }) {
          const { $from } = editor.state.selection;
          const mark = $from
            .marks()
            .find((m) => m.type.name === "comment") as
            | { attrs: { threadId?: string } }
            | undefined;
          if (mark?.attrs.threadId) {
            setActiveThreadId(mark.attrs.threadId);
            setSidebarOpen(true);
          }
        },
      },
      [ydoc, role],
    );

    // startThread anchors a new comment thread to the current selection: it
    // marks the selected range with a fresh threadId (a doc edit, synced
    // like any other) and opens the sidebar on it with the compose box
    // ready — the thread's first comment record isn't written until the
    // user actually submits it (CommentSidebar's postReply), so an aborted
    // "Comment" click just leaves an unused mark rather than a stray
    // record.
    function startThread() {
      if (!editor) return;
      const { from, to, empty } = editor.state.selection;
      if (empty) return;
      const quotedText = editor.state.doc.textBetween(from, to, " ");
      const threadId = crypto.randomUUID();
      editor.chain().focus().setComment(threadId).run();
      setPendingThread({ threadId, quotedText });
      setActiveThreadId(threadId);
      setSidebarOpen(true);
    }

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex overflow-hidden">
          <div className="flex-1 flex flex-col items-center overflow-y-auto">
            <EditorContent className="w-full flex-1" editor={editor} />
          </div>
          {sidebarOpen && (
            <CommentSidebar
              docId={uri}
              editor={editor}
              activeThreadId={activeThreadId}
              pendingThread={pendingThread}
              onPendingThreadResolved={() => setPendingThread(null)}
              onClose={() => setSidebarOpen(false)}
            />
          )}
        </div>
        <PageHeader>
          <div className="flex gap-2">
            {role === "editor" && (
              <Button variant="ghost" size="sm" onClick={startThread}>
                Comment
              </Button>
            )}
            {role === "editor" && !currentOrg && (
              <ShareDialog
                grantees={grantees}
                isAdding={isAddingPermission}
                roles
                currentUserDid={currentUserDid}
                onAddPermission={(actors, role) =>
                  addPermission({ actors, role })
                }
                onRemovePermission={(actor) => removePermission(actor)}
              />
            )}
            <HelpDialog />
          </div>
        </PageHeader>
      </div>
    );
  },
  pendingComponent: () => <article>Loading...</article>,
});
