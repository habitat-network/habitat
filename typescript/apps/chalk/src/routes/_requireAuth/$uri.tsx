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
import { useEffect, useState, type MouseEvent } from "react";
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
import { CommentSidebar, type PendingAnchor } from "@/components/CommentSidebar";
import {
  CommentHighlight,
  encodeAnchor,
  type CommentAnchor,
} from "@/extensions/commentAnchor";
import { useYDoc } from "@/hooks/useYDoc";
import { Route as RequireAuthRoute } from "@/routes/_requireAuth";
import {
  getDocInitialState,
  getDocRole,
  listComments,
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
    // from activeCommentUri: closing the sidebar shouldn't forget which
    // thread was selected, and clicking a highlight should reopen it.
    const [sidebarOpen, setSidebarOpen] = useState(false);
    const [activeCommentUri, setActiveCommentUri] = useState<string | null>(
      null,
    );
    const [pendingAnchor, setPendingAnchor] = useState<PendingAnchor | null>(
      null,
    );

    // Shared with CommentSidebar via the same react-query cache entry
    // (identical queryKey) rather than prop-drilled — only the anchor
    // fields are needed here, to keep the editor's highlights in sync.
    const { data: commentsData } = useQuery({
      queryKey: ["comments", uri],
      queryFn: () => listComments({ data: { docId: uri } }),
    });

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
          CommentHighlight,
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
      },
      [ydoc, role],
    );

    // Keep the editor's comment-range highlights in sync with the current
    // comment list (plus any not-yet-created pending selection) whenever
    // either changes. Highlights are computed decorations, not a mutation
    // of the shared doc — see commentAnchor.ts's CommentHighlight — so
    // this is purely a read, safe to rerun on every render of new data.
    useEffect(() => {
      if (!editor) return;
      const anchors: CommentAnchor[] = (commentsData?.comments ?? []).map(
        (c) => ({ uri: c.uri, anchorStart: c.anchorStart, anchorEnd: c.anchorEnd }),
      );
      if (pendingAnchor) {
        anchors.push({
          uri: "pending",
          anchorStart: pendingAnchor.anchorStart,
          anchorEnd: pendingAnchor.anchorEnd,
        });
      }
      editor.commands.setCommentHighlights(anchors);
    }, [editor, commentsData, pendingAnchor]);

    // startThread captures the current selection's CRDT anchor (see
    // encodeAnchor) and opens the sidebar on it with the compose box
    // ready — the thread's root comment record isn't written until the
    // user actually submits it (CommentSidebar's startFirstComment), so an
    // aborted "Comment" click just leaves the selection alone.
    function startThread() {
      if (!editor) return;
      const { from, to, empty } = editor.state.selection;
      if (empty) return;
      const anchor = encodeAnchor(editor.state, from, to);
      if (!anchor) return;
      const quotedText = editor.state.doc.textBetween(from, to, " ");
      setPendingAnchor({ ...anchor, quotedText });
      setActiveCommentUri(null);
      setSidebarOpen(true);
    }

    // handleEditorClick opens the sidebar on the thread whose highlight was
    // clicked, the same way clicking a comment bubble does in most doc
    // editors — highlights are plain decorations (see CommentHighlight),
    // so this reads the DOM attribute they render rather than a
    // ProseMirror mark.
    function handleEditorClick(e: MouseEvent<HTMLDivElement>) {
      const target = (e.target as HTMLElement).closest<HTMLElement>(
        "[data-comment-uri]",
      );
      const commentUri = target?.dataset.commentUri;
      if (commentUri && commentUri !== "pending") {
        setActiveCommentUri(commentUri);
        setSidebarOpen(true);
      }
    }

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex overflow-hidden">
          <div
            className="flex-1 flex flex-col items-center overflow-y-auto"
            onClick={handleEditorClick}
          >
            <EditorContent className="w-full flex-1" editor={editor} />
          </div>
          {sidebarOpen && (
            <CommentSidebar
              docId={uri}
              activeCommentUri={activeCommentUri}
              pendingAnchor={pendingAnchor}
              onPendingAnchorResolved={() => setPendingAnchor(null)}
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
