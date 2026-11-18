import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { createFileRoute } from "@tanstack/react-router";
import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useMemo, useState, type MouseEvent } from "react";
import {
  ShareDialog,
  type Actor,
  type ShareDialogGrantee,
  type ShareDialogRole,
} from "internal";
import { Button, toast } from "internal/components/ui";
import { useActors } from "internal/hooks";
import { HelpDialog } from "@/components/docs/HelpDialog";
import {
  CommentSidebar,
  type PendingAnchor,
} from "@/components/docs/CommentSidebar";
import { PageHeader } from "@/components/shell/PageHeader";
import {
  CommentHighlight,
  encodeAnchor,
  type CommentAnchor,
} from "@/extensions/commentAnchor";
import { OrgShareControl } from "@/components/docs/OrgShareControl";
import { useYDoc } from "@/hooks/useYDoc";
import { groupLabel, groupQuery } from "@/lib/queries";
import {
  getDocInitialState,
  getDocRole,
  getDocSummary,
  listComments,
  listDocAccess,
  revokeDocAccess,
  shareDoc,
} from "@/server/fns/docs";

const docRoleQueryOptions = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["docRole", org, docId],
    queryFn: () => getDocRole({ data: { org, docId } }),
  });

const docInitialStateQueryOptions = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["docInitialState", org, docId],
    queryFn: () => getDocInitialState({ data: { org, docId } }),
  });

// Habitat server the share dialog's user search resolves handles against.
// VITE_HABITAT_DOMAIN is only set by moon's dev task (moon.yml), pointing at
// local pear — the only server that knows local handles. Production builds
// leave it unset, so searchActorsTypeahead falls back to its default.
const identityResolverUrl = import.meta.env.VITE_HABITAT_DOMAIN
  ? `https://${import.meta.env.VITE_HABITAT_DOMAIN}`
  : undefined;

// caretColor derives a stable per-user cursor color from the DID, so a given
// person looks the same to everyone without any coordination.
function caretColor(did: string): string {
  let hash = 0;
  for (const ch of did) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
  return `hsl(${hash % 360} 70% 45%)`;
}

// The doc editor (chalk's): a tiptap editor over the doc's Y.Doc, synced
// through DocRoom, with comment threads and sharing. $docId is the doc's
// space URI.
export const Route = createFileRoute("/_member/g/$org/docs/$docId")({
  loader: async ({ context, params }) => {
    const [role, initialState] = await Promise.all([
      context.queryClient.ensureQueryData(
        docRoleQueryOptions(params.org, params.docId),
      ),
      context.queryClient.ensureQueryData(
        docInitialStateQueryOptions(params.org, params.docId),
      ),
    ]);
    return { role, initialState };
  },
  component() {
    const { org, docId: uri } = Route.useParams();
    const { member } = Route.useRouteContext();
    const currentUserDid = member.did;
    const { role, initialState } = Route.useLoaderData();
    const { data: group } = useQuery(groupQuery(org));
    const { data: summary } = useQuery({
      queryKey: ["docSummary", org, uri],
      queryFn: () => getDocSummary({ data: { org, docId: uri } }),
    });
    const self = {
      displayName: group?.me.displayName ?? null,
      handle: member.handle,
    };
    const { ydoc, awareness } = useYDoc(uri, initialState);
    // Editors and commenters may write comments; a viewer may only read
    // them. Mirrors canComment in functions.ts, which is what actually
    // rejects a viewer's createComment — this only decides what the UI
    // offers.
    const canComment = role === "editor" || role === "commenter";
    const queryClient = useQueryClient();

    const accessQueryKey = ["docAccess", org, uri];
    // listDocAccess only returns DIDs and the tier each one holds;
    // resolving DIDs to handles/avatars for display is useActors's job.
    const { data: access = [] } = useQuery({
      queryKey: accessQueryKey,
      queryFn: () => listDocAccess({ data: { org, docId: uri } }),
    });
    // Memoized so useActors' query key doesn't churn on every render.
    const accessDids = useMemo(() => access.map((a) => a.did), [access]);
    const getActor = useActors(accessDids);
    // Not memoized: getActor's identity never changes, but what it returns
    // does once a profile resolves, so a memo would keep showing bare DIDs.
    const grantees: ShareDialogGrantee[] = access.map((a) => ({
      ...getActor(a.did),
      role: a.role,
    }));
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
              shareDoc({
                data: { org, docId: uri, subjectDid: actor.did, role },
              }),
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
        revokeDocAccess({ data: { org, docId: uri, subjectDid: actor.did } }),
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
    // hasSelection drives the toolbar button's label/behavior: with text
    // selected it starts a new thread ("Add comment"), otherwise it just
    // opens the sidebar on whatever's already there ("Comments").
    const [hasSelection, setHasSelection] = useState(false);

    // Shared with CommentSidebar via the same react-query cache entry
    // (identical queryKey) rather than prop-drilled — only the anchor
    // fields are needed here, to keep the editor's highlights in sync.
    const { data: commentsData } = useQuery({
      queryKey: ["comments", org, uri],
      queryFn: () => listComments({ data: { org, docId: uri } }),
    });

    const caretName = self.displayName ?? self.handle ?? currentUserDid;
    const caretUser = useMemo(
      () => ({ name: caretName, color: caretColor(currentUserDid) }),
      [caretName, currentUserDid],
    );

    const editor = useEditor(
      {
        // The doc's real content comes from ydoc over the WebSocket, not
        // server-rendered markup — rendering on the server would touch
        // `document`, which doesn't exist there, and would just be thrown
        // away on hydration anyway.
        immediatelyRender: false,
        // Client-side only: keeps the editor read-only for anyone below
        // editor — a commenter can add comments but not change the
        // document itself. The actual access gate is the WS route's reader
        // check (ws.$docId.ts) — this doesn't stop write attempts made
        // outside the UI.
        editable: role === "editor",
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
          // Remote users' cursors/selections, labelled with their name.
          // Awareness is relayed between sockets by DocRoom.
          CollaborationCaret.configure({
            provider: { awareness },
            user: caretUser,
          }),
          CommentHighlight,
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
        onSelectionUpdate({ editor }) {
          setHasSelection(!editor.state.selection.empty);
        },
      },
      [ydoc, awareness, role, caretUser],
    );

    // Keep the editor's comment-range highlights in sync with the current
    // comment list (plus any not-yet-created pending selection) whenever
    // either changes. Highlights are computed decorations, not a mutation
    // of the shared doc — see commentAnchor.ts's CommentHighlight — so
    // this is purely a read, safe to rerun on every render of new data.
    useEffect(() => {
      // isDestroyed, not just truthiness: React's dev-mode double-invoke
      // (mount, cleanup/destroy, remount) can leave this closure holding a
      // stale `editor` whose internal commandManager tiptap's destroy()
      // has already nulled out — `editor.commands` throws in that case
      // rather than just being falsy.
      if (!editor || editor.isDestroyed) return;
      const anchors: CommentAnchor[] = (commentsData?.comments ?? []).map(
        (c) => ({
          uri: c.uri,
          anchorStart: c.anchorStart,
          anchorEnd: c.anchorEnd,
        }),
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
      <div className="flex h-[calc(100svh-1rem)] flex-col-reverse">
        <div className="flex-1 flex overflow-hidden">
          <div
            className="flex-1 flex flex-col items-center overflow-y-auto"
            onClick={handleEditorClick}
          >
            <EditorContent className="w-full flex-1" editor={editor} />
          </div>
          {sidebarOpen && (
            <CommentSidebar
              org={org}
              docId={uri}
              currentUserDid={currentUserDid}
              canComment={canComment}
              activeCommentUri={activeCommentUri}
              onSelectComment={setActiveCommentUri}
              pendingAnchor={pendingAnchor}
              onPendingAnchorResolved={() => setPendingAnchor(null)}
              onClose={() => {
                setSidebarOpen(false);
                // Closing without submitting abandons the pending
                // selection — otherwise its highlight (and the sidebar's
                // "no comments yet" fallback) would linger after the user
                // clearly walked away from it.
                setPendingAnchor(null);
              }}
            />
          )}
        </div>
        <PageHeader
          title={summary?.title || "Untitled"}
          description={group ? groupLabel(group) : undefined}
        >
          <div className="flex gap-2">
            {canComment ? (
              <Button
                variant="ghost"
                onClick={
                  hasSelection ? startThread : () => setSidebarOpen(true)
                }
              >
                {hasSelection ? "Add comment" : "Comments"}
              </Button>
            ) : (
              // A viewer can read the thread list but not start one, so
              // the button never offers "Add comment" to them.
              <Button variant="ghost" onClick={() => setSidebarOpen(true)}>
                Comments
              </Button>
            )}
            {role === "editor" && (
              <ShareDialog
                grantees={grantees}
                isAdding={isAddingPermission}
                roles
                currentUserDid={currentUserDid}
                identityResolverUrl={identityResolverUrl}
                onAddPermission={(actors, role) =>
                  addPermission({ actors, role })
                }
                onRemovePermission={(actor) => removePermission(actor)}
              >
                {group && (
                  <OrgShareControl
                    org={org}
                    docId={uri}
                    orgName={groupLabel(group)}
                  />
                )}
              </ShareDialog>
            )}
            <HelpDialog />
          </div>
        </PageHeader>
      </div>
    );
  },
  pendingComponent: () => (
    <p className="p-8 text-sm text-muted-foreground">Loading…</p>
  ),
});
