import { Editor, EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { createLibp2p, Libp2p } from "libp2p";
import { webSockets } from "@libp2p/websockets";
import { circuitRelayTransport } from "@libp2p/circuit-relay-v2";
import { multiaddr } from "@multiformats/multiaddr";
import { noise } from "@chainsafe/libp2p-noise";
import { yamux } from "@chainsafe/libp2p-yamux";
import { identify } from "@libp2p/identify";
import { gossipsub } from "@chainsafe/libp2p-gossipsub";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { Libp2pConnectionProvider } from "@/connectionProvider";
import { webRTC } from "@libp2p/webrtc";
import { webTransport } from "@libp2p/webtransport";
import { peerIdFromString } from "@libp2p/peer-id";
import {
  addPermissionMutationOptions,
  docEditsQueryOptions,
  docQueryOptions,
  editorProfilesQueryOptions,
} from "@/queries/docs";
import { Route as AuthRoute } from "@/routes/_requireAuth";
import { ShareDialog, AuthManager, query, XRPCError, procedure } from "internal";
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
  Spinner,
} from "internal/components/ui";
import { HelpDialog } from "@/components/HelpDialog";
import { PageHeader } from "@/components/PageHeader";
import { CheckIcon } from "lucide-react";

const habitatDID = "did:plc:ss2uhsajrstfhkq73fteu4zz";

async function startPeerDiscovery(
  uri: string,
  relayPeerId: string,
  node: Libp2p,
  authManager: AuthManager,
): Promise<void> {
  try {
    const stream = await node.dialProtocol(
      peerIdFromString(relayPeerId),
      "/habitat/peer-discovery/1.0.0",
    );
    const { token: serviceAuthToken } = await query(
      "com.atproto.server.getServiceAuth",
      {
        lxm: "com.atproto.server.getServiceAuth",
        aud: habitatDID,
      },
      { authManager: authManager },
    );

    const encoder = new TextEncoder();
    stream.sink(
      (async function* () {
        yield encoder.encode(
          JSON.stringify({
            topic: uri,
            serviceauth_token: serviceAuthToken,
          }),
        );
      })(),
    );

    async function dialPeer(peerIdStr: string): Promise<void> {
      if (peerIdStr === node.peerId.toString()) return;
      if (peerIdStr === relayPeerId) return;
      if (node.getConnections(peerIdFromString(peerIdStr)).length > 0) return;
      const circuitAddr = multiaddr(
        `/p2p/${relayPeerId}/p2p-circuit/p2p/${peerIdStr}`,
      );
      try {
        await node.dial(circuitAddr);
      } catch (e) {
        console.log("caught error dialing", e, peerIdStr);
      }
    }

    const decoder = new TextDecoder();
    let buf = "";
    for await (const chunk of stream.source) {
      const bytes = chunk instanceof Uint8Array ? chunk : chunk.subarray();
      buf += decoder.decode(bytes, { stream: true });
      const lines = buf.split("\n");
      buf = lines.pop() ?? "";
      for (const line of lines) {
        const id = line.trim();
        dialPeer(id).catch((e) => {
          console.log("error dialing peer: ", e);
        });
      }
    }
  } catch { }
}

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const ydoc = new Y.Doc();
    // Fetch the record
    const { uri } = params;
    const [, , docDID, , rkey] = uri.split("/");
    const data = await context.queryClient.fetchQuery(
      docQueryOptions(uri, context.authManager),
    );

    if (data.value.blob) {
      Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(data.value.blob));
    }

    const edits = await context.queryClient.fetchQuery(
      docEditsQueryOptions(data, context.authManager),
    );
    for (const e of edits) {
      if (e?.value.blob) {
        Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(e.value.blob));
      }
    }

    // setup libp2p
    const node = await createLibp2p({
      addresses: {
        listen: ["/p2p-circuit", "/webrtc"],
      },
      transports: [
        webRTC({
          rtcConfiguration: {
            iceServers: [{ urls: ["stun:stun.l.google.com:19302"] }],
          },
        }),
        webTransport(),
        webSockets(),
        circuitRelayTransport(),
      ],
      connectionEncrypters: [noise()],
      streamMuxers: [yamux()],
      services: {
        identify: identify(),
        pubsub: gossipsub({
          runOnLimitedConnection: true,
          allowPublishToZeroTopicPeers: true,
          emitSelf: false,
        }),
      },
    });

    // TODO: this should look up the habitat service on the doc owner's DID and use that endpoint, not the generic __HABITAT_DOMAIN__. This is left for later.
    // All user pear nodes are expected to implement the relay address.
    const domain = __HABITAT_DOMAIN__;
    const relayAddr = multiaddr(`/dns4/${domain}/tcp/443/wss`);
    const conn = await node.dial(relayAddr);
    let relayPeerId = conn.remotePeer.toString();

    void startPeerDiscovery(uri, relayPeerId, node, context.authManager).catch(
      () => { },
    );

    const provider = new Libp2pConnectionProvider(node, ydoc, uri);

    return {
      provider,
      node,
      ydoc,
      rkey,
      docDID: docDID,
      record: data.value,
      docPermissions: data.permissions,
    };
  },
  onLeave({ loaderData }) {
    loaderData?.provider.destroy();
    loaderData?.ydoc.destroy();
    loaderData?.node.stop();
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const { docDID, rkey, ydoc, provider, node, record, docPermissions } =
      Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const { profile } = AuthRoute.useLoaderData();
    const [dirty, setDirty] = useState(false);
    const { data: editorProfiles } = useQuery(
      editorProfilesQueryOptions(record.editorClique, authManager),
    );
    const { mutate: save } = useMutation({
      mutationFn: async ({ editor }: { editor: Editor }) => {
        const did = authManager.getAuthInfo()?.did;
        const heading = editor.$node("heading")?.textContent;
        const collection =
          docDID === did ? "network.habitat.docs" : "network.habitat.docs.edit";
        const mappedKey = docDID === did ? rkey : `${docDID}-${rkey}`;

        await procedure(
          "network.habitat.putRecord",
          {
            repo: did!,
            collection: collection,
            rkey: mappedKey,
            record: {
              name: heading ?? "Untitled",
              blob: Y.encodeStateAsUpdateV2(ydoc).toBase64(),
              editorClique: record.editorClique,
            },
            grantees: docPermissions,
          },
          { authManager },
        )
      },

      onSuccess: () => setDirty(false),
    });
    const { mutate: addPermission, isPending: isAddingPermission } =
      useMutation(addPermissionMutationOptions(authManager));
    // debounce
    const handleUpdate = useMemo(() => {
      let prevTimeout: number | undefined;
      return ({ editor }: { editor: Editor }) => {
        setDirty(true);
        clearTimeout(prevTimeout);
        prevTimeout = window.setTimeout(() => {
          save({ editor });
        }, 1000);
      };
    }, [save]);
    const editor = useEditor(
      {
        extensions: [
          StarterKit.configure({
            undoRedo: false,
          }),
          Collaboration.configure({
            document: ydoc,
          }),
          CollaborationCaret.configure({
            provider,
            user: {
              name: profile.handle,
              color: "#f783ac",
            },
          }),
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-20rem))] py-8 outline-none",
          },
        },
        onUpdate: handleUpdate,
      },
      [ydoc],
    );
    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <Popover>
            <PopoverTrigger
              render={
                <Button size="icon" variant="outline">
                  {dirty ? <Spinner /> : <CheckIcon />}
                </Button>
              }
            />
            <PopoverContent>
              <PopoverTitle>Sync status</PopoverTitle>
              <span>{dirty ? "🔄 Syncing" : "✅ Synced"}</span>
              <PopoverTitle>Peer info</PopoverTitle>
              <span className="break-all">
                Node id: {node.peerId.toString()}
              </span>
            </PopoverContent>
          </Popover>
          <HelpDialog />

          {docDID === authManager.getAuthInfo()?.did && record.editorClique && (
            <ShareDialog
              isAdding={isAddingPermission}
              grantees={editorProfiles ?? []}
              authManager={authManager}
              onAddPermission={(actors) =>
                addPermission({
                  grantees: actors.map((actor) => actor.did),
                  editorCliqueUri: record.editorClique,
                })
              }
            />
          )}
        </PageHeader>
      </div>
    );
  },
  errorComponent({ error }) {
    if (error instanceof XRPCError) {
      if (error.status === 403) {
        return <p>you do not have permission to view this doc</p>;
      }
    }
    return <p>Something went wrong.</p>;
  },
  pendingComponent: () => <article>Loading...</article>,
});

// ES2024 Uint8Array base64 methods (polyfill types for TypeScript < 5.7)
declare global {
  interface Uint8Array {
    toBase64(options?: { alphabet?: "base64" | "base64url" }): string;
  }

  interface Uint8ArrayConstructor {
    fromBase64(
      string: string,
      options?: {
        alphabet?: "base64" | "base64url";
        lastChunkHandling?: "loose" | "strict" | "stop-before-partial";
      },
    ): Uint8Array;
  }
}
