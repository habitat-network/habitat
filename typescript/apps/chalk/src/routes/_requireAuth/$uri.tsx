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
import { useEffect } from "react";
import {
  ShareDialog,
  getProfiles,
  type Actor,
  type ShareDialogGrantee,
  type ShareDialogRole,
} from "internal";
import { toast } from "internal/components/ui";
import { PageHeader } from "@/components/PageHeader";
import { HelpDialog } from "@/components/HelpDialog";
import { useYDoc } from "@/hooks/useYDoc";
import {
  getDoc,
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

const docQueryOptions = (docId: string) =>
  queryOptions({
    queryKey: ["doc", docId],
    queryFn: () => getDoc({ data: { docId } }),
  });

export const Route = createFileRoute("/_requireAuth/$uri")({
  loader: async ({ context, params }) => {
    const [role, initialState, doc] = await Promise.all([
      context.queryClient.ensureQueryData(docRoleQueryOptions(params.uri)),
      context.queryClient.ensureQueryData(
        docInitialStateQueryOptions(params.uri),
      ),
      context.queryClient.ensureQueryData(docQueryOptions(params.uri)),
    ]);
    return { role, initialState, doc };
  },
  component() {
    const { uri } = Route.useParams();
    const { did: currentUserDid } = Route.useRouteContext();
    const { role, initialState, doc } = Route.useLoaderData();
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

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <div className="flex gap-2">
            {role === "editor" && !doc?.isOrg && (
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
