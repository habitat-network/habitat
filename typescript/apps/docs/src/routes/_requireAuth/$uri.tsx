import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState, useEffect } from "react";
import { createLibp2p, Libp2p } from "libp2p";
import { webSockets } from "@libp2p/websockets";
import { circuitRelayTransport } from "@libp2p/circuit-relay-v2";
import { multiaddr } from "@multiformats/multiaddr";
import { PeerId } from "@libp2p/interface";
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
  removePermissionMutationOptions,
  docEditsQueryOptions,
  docQueryOptions,
  docsListQueryOptions,
  editorProfilesQueryOptions,
} from "@/queries/docs";
import {
  ShareDialog,
  AuthManager,
  query,
  XRPCError,
  procedure,
} from "internal";
import {
  AvatarGroup,
  Button,
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
  Spinner,
} from "internal/components/ui";
import { UserAvatar } from "internal";
import { HelpDialog } from "@/components/HelpDialog";
import { PageHeader } from "@/components/PageHeader";
import { CheckIcon } from "lucide-react";
import { profileQueryOptions } from "@/queries/profile";

const habitatDID = "did:plc:ss2uhsajrstfhkq73fteu4zz";

async function startPeerDiscovery(
  uri: string, // The document uri
  relayPeerId: PeerId,
  node: Libp2p,
  authManager: AuthManager,
): Promise<void> {
  try {
    const stream = await node.dialProtocol(
      relayPeerId,
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
      if (peerIdStr === relayPeerId.toString()) return;
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

// Preserve ydocs across navigations so the editor never flashes stale content.
// Re-applying the backend blob on each load is safe — YJS updates are idempotent.
const ydocRegistry = new Map<string, Y.Doc>();

function getHeadingFromYdoc(ydoc: Y.Doc): string | undefined {
  const fragment = ydoc.getXmlFragment("default");
  for (const child of fragment.toArray()) {
    if (child instanceof Y.XmlElement && child.nodeName === "heading") {
      return child
        .toArray()
        .filter((n): n is Y.XmlText => n instanceof Y.XmlText)
        .map((n) => n.toJSON())
        .join("");
    }
  }
  return undefined;
}

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const { uri } = params;

    // Reuse an existing ydoc for this URI if available so the editor never
    // has to reinitialize from scratch (prevents visible content flash).
    const existingYdoc = ydocRegistry.get(uri);
    const ydoc = existingYdoc ?? new Y.Doc();
    if (!existingYdoc) {
      ydocRegistry.set(uri, ydoc);
    }

    // Always re-apply the backend state — YJS CRDT merges are idempotent,
    // so this picks up any changes made by other users without overwriting
    // local edits that are ahead of the last save.
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

    async function dialRelayAndStartPeerDiscovery() {
      const connections = node.getConnections();
      if (
        connections.some((conn) => {
          return conn.remoteAddr.toString() === relayAddr.toString();
        })
      ) {
        // Already connected to relay
        return;
      }
      try {
        const conn = await node.dial(relayAddr);
        const relayPeerId = conn.remotePeer;
        void startPeerDiscovery(uri, relayPeerId, node, context.authManager);
      } catch {
        // Relay unreachable — document still works, real-time collaboration unavailable
        // TODO: can we signal to the user somehow that real-time collaboration is not working ?
        console.error("unable to connect to habitat relay; continuing without real-time collaboration")
      }
    }

    await dialRelayAndStartPeerDiscovery();
    const provider = new Libp2pConnectionProvider(node, ydoc, uri);

    const profile = await context.queryClient.fetchQuery(
      profileQueryOptions(
        context.authManager.getAuthInfo()!.did,
        context.authManager,
      ),
    );

    return {
      provider,
      node,
      ydoc,
      doc: data,
      uri,
      profile,
      dialRelayAndStartPeerDiscovery,
    };
  },
  onLeave({ loaderData }) {
    loaderData?.provider.destroy();
    loaderData?.node.stop();
    // ydoc is intentionally kept alive in ydocRegistry so navigating back
    // reuses the same in-memory state without a content flash.
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const {
      ydoc,
      provider,
      node,
      doc,
      uri,
      profile,
      dialRelayAndStartPeerDiscovery,
    } = Route.useLoaderData();
    const [, , docDID, , rkey] = uri.split("/");
    const { authManager, queryClient } = Route.useRouteContext();
    const did = authManager.getAuthInfo()?.did;

    useEffect(() => {
      async function handleVisibilityChange() {
        // When the page becomes visible again, reconnect to the relay and fetch any updates that may have happened since
        if (document.visibilityState !== "visible") return;
        await dialRelayAndStartPeerDiscovery();
      }
      document.addEventListener("visibilitychange", handleVisibilityChange);
      return () =>
        document.removeEventListener(
          "visibilitychange",
          handleVisibilityChange,
        );
    }, [dialRelayAndStartPeerDiscovery]);

    useEffect(() => {
      const syncHeading = () => {
        const name = getHeadingFromYdoc(ydoc) ?? "Untitled";
        queryClient.setQueryData(
          docsListQueryOptions(authManager).queryKey,
          (old) => {
            if (!old) return old;
            return {
              ...old,
              records: old.records.map((r) =>
                r.uri === uri ? { ...r, value: { ...r.value, name } } : r
              ),
            };
          }
        );
      };
      ydoc.on("update", syncHeading);
      syncHeading();
      return () => ydoc.off("update", syncHeading);
    }, [ydoc, uri, queryClient, authManager]);

    const [dirty, setDirty] = useState(false);
    const { data: editorProfiles } = useQuery(
      editorProfilesQueryOptions(doc.value.editorClique, authManager),
    );
    const { mutate: save } = useMutation({
      mutationFn: async () => {
        const heading = getHeadingFromYdoc(ydoc);
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
              editorClique: doc.value.editorClique,
            },
            grantees: doc.permissions,
          },
          { authManager },
        );
      },

      onSuccess: () => {
        setDirty(false);
      },
    });
    const { mutate: addPermission, isPending: isAddingPermission } =
      useMutation(addPermissionMutationOptions(authManager));
    const { mutate: removePermission } = useMutation(
      removePermissionMutationOptions(authManager),
    );
    // debounce
    const handleUpdate = useMemo(() => {
      let prevTimeout: number | undefined;
      return () => {
        setDirty(true);
        clearTimeout(prevTimeout);
        prevTimeout = window.setTimeout(() => {
          save();
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
          <div className="flex items-center gap-2">
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
            {editorProfiles && editorProfiles.length > 0 && (
              <div className="flex items-center gap-1">
                {editorProfiles.find((p) => p.did === docDID) && (
                  <UserAvatar
                    actor={editorProfiles.find((p) => p.did === docDID)!}
                    size="sm"
                    className="ring-2 ring-foreground"
                  />
                )}
                {editorProfiles.filter((p) => p.did !== docDID).length > 0 && (
                  <AvatarGroup>
                    {editorProfiles
                      .filter((p) => p.did !== docDID)
                      .map((p) => (
                        <UserAvatar key={p.did} actor={p} size="sm" />
                      ))}
                  </AvatarGroup>
                )}
              </div>
            )}
          </div>
          <HelpDialog />

          {docDID === authManager.getAuthInfo()?.did &&
            doc.value.editorClique && (
              <ShareDialog
                isAdding={isAddingPermission}
                grantees={(editorProfiles ?? []).filter(
                  (p) => p.did !== authManager.getAuthInfo()?.did,
                )}
                authManager={authManager}
                onAddPermission={(actors) =>
                  addPermission({
                    grantees: actors.map((actor) => actor.did),
                    editorCliqueUri: doc.value.editorClique,
                  })
                }
                onRemovePermission={(actor) =>
                  removePermission({
                    grantee: actor.did,
                    editorCliqueUri: doc.value.editorClique,
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
        return <p>You do not have access to this doc</p>;
      }
    }
    console.error(error)
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
