import { Editor, EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { createLibp2p } from "libp2p";
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
import { bootstrap } from "@libp2p/bootstrap";
import { peerIdFromString } from "@libp2p/peer-id";

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const relayAddr = multiaddr(`/dns4/${__HABITAT_DOMAIN__}/tcp/443/wss`);
    // setup libp2p
    const node = await createLibp2p({
      addresses: {
        listen: ["/p2p-circuit"], // ADD THIS
      },
      transports: [webSockets(), circuitRelayTransport()],
      peerDiscovery: [
        bootstrap({
          list: [relayAddr.toString()],
        }) as any,
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

    const conn = await node.dial(relayAddr);
    const relayPeerId = conn.remotePeer.toString();

    // fetch original record
    const { uri } = params;
    const [, , docDID, lexicon, rkey] = uri.split("/");
    const originalRecordResponse = await context.authManager.fetch(
      `/xrpc/network.habitat.getRecord?repo=${docDID}&collection=${lexicon}&rkey=${rkey}&includePermissions=true`,
    );

    // The gossipsub topic is also used as the per-document rendezvous key.
    const habitatUri = `habitat://${docDID}/network.habitat.docs/${rkey}`;

    function registerWithRelay() {
      fetch(`https://${__HABITAT_DOMAIN__}/p2p/peers`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          peerId: node.peerId.toString(),
          topic: habitatUri,
        }),
      }).catch(() => { });
    }

    // Register now and re-register whenever the relay connection is re-established,
    // since the relay removes us from the registry when our connection drops.
    registerWithRelay();
    node.addEventListener("peer:connect", (event) => {
      if (event.detail.toString() === relayPeerId) {
        registerWithRelay();
      }
    });

    async function discoverAndDialPeers(): Promise<void> {
      let peerIds: string[];
      try {
        const res = await fetch(
          `https://${__HABITAT_DOMAIN__}/p2p/peers?topic=${encodeURIComponent(habitatUri)}`,
        );
        if (!res.ok) return;
        const data: { peers: string[] } = await res.json();
        peerIds = data.peers;
      } catch {
        return;
      }
      for (const peerIdStr of peerIds) {
        // Don't redial self or relay
        if (peerIdStr === node.peerId.toString()) continue;
        if (peerIdStr === relayPeerId) continue;
        if (node.getConnections(peerIdFromString(peerIdStr)).length > 0)
          continue;

        const circuitAddr = multiaddr(
          `/p2p/${relayPeerId}/p2p-circuit/p2p/${peerIdStr}`,
        );
        try {
          await node.dial(circuitAddr);
        } catch (e) {
          console.log("caught error dialing", e, peerIdStr);
          // reservation not ready or peer left — retry next tick
        }
      }
    }

    await discoverAndDialPeers();
    const discoveryInterval = window.setInterval(
      discoverAndDialPeers,
      500 /* 500ms for testing */,
    );

    const data: {
      uri: string;
      cid: string;
      value: HabitatDoc;
      permissions?: Array<{ $type: string; did?: string; uri?: string }>;
    } = await originalRecordResponse?.json();

    const ydoc = new Y.Doc();
    if (data.value.blob) {
      Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(data.value.blob));
    }

    const did = context.authManager.getAuthInfo()?.did;

    // editRkey is the rkey used in network.habitat.docs.edit for this doc
    const editRkey = `${docDID}-${rkey}`;

    const granteeDIDs = (data.permissions ?? [])
      .filter(
        (
          g,
        ): g is { $type: "network.habitat.grantee#didGrantee"; did: string } =>
          g.$type === "network.habitat.grantee#didGrantee" && !!g.did,
      )
      .map((g) => g.did);

    async function fetchAndMerge() {
      // Re-fetch the owner's latest doc (may have changed since initial load)
      try {
        const res = await context.authManager.fetch(
          `/xrpc/network.habitat.getRecord?repo=${docDID}&collection=${lexicon}&rkey=${rkey}`,
        );
        if (res?.ok) {
          const latest: { value: HabitatDoc } = await res.json();
          if (latest.value.blob) {
            Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(latest.value.blob));
          }
        }
      } catch {
        /* silently skip */
      }

      // Fetch and merge edits from all DID grantees (only the owner sees the full list)
      await Promise.all(
        granteeDIDs.map(async (granteeDID) => {
          try {
            const res = await context.authManager.fetch(
              `/xrpc/network.habitat.getRecord?repo=${granteeDID}&collection=network.habitat.docs.edit&rkey=${encodeURIComponent(editRkey)}`,
            );
            if (!res?.ok) return;
            const editData: { value: HabitatDoc } = await res.json();
            if (editData.value.blob) {
              Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(editData.value.blob));
            }
          } catch {
            /* silently skip */
          }
        }),
      );

      // If non-owner and not already in the grantee list, also fetch own edit
      if (docDID !== did && !granteeDIDs.includes(did!)) {
        try {
          const res = await context.authManager.fetch(
            `/xrpc/network.habitat.getRecord?repo=${did}&collection=network.habitat.docs.edit&rkey=${encodeURIComponent(editRkey)}`,
          );
          if (res?.ok) {
            const editData: { value: HabitatDoc } = await res.json();
            if (editData.value.blob) {
              Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(editData.value.blob));
            }
          }
        } catch {
          /* silently skip */
        }
      }

      // Write back the merged state so others see convergence
      try {
        await context.authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection:
              docDID === did
                ? "network.habitat.docs"
                : "network.habitat.docs.edit",
            rkey: docDID === did ? rkey : editRkey,
            record: {
              name: data.value.name ?? "Untitled",
              blob: Y.encodeStateAsUpdateV2(ydoc).toBase64(),
            },
          }),
        );
      } catch {
        /* silently skip — will be saved on next edit */
      }
    }

    await fetchAndMerge();

    function onVisibilityChange() {
      if (document.visibilityState === "visible") {
        registerWithRelay()
        fetchAndMerge().catch(() => { });
      }
    }
    document.addEventListener("visibilitychange", onVisibilityChange);

    const provider = new Libp2pConnectionProvider(node, ydoc, habitatUri);

    return {
      provider,
      node,
      ydoc,
      rkey,
      did: did,
      docDID: docDID,
      discoveryInterval,
      onVisibilityChange,
    };
  },
  onLeave({ loaderData }) {
    if (loaderData?.discoveryInterval !== undefined) {
      window.clearInterval(loaderData.discoveryInterval);
    }
    if (loaderData?.onVisibilityChange) {
      document.removeEventListener(
        "visibilitychange",
        loaderData.onVisibilityChange,
      );
    }
    loaderData?.provider.destroy();
    loaderData?.ydoc.destroy();
    loaderData?.node.stop();
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const { did, docDID, rkey, ydoc, provider, node } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const [dirty, setDirty] = useState(false);
    const { mutate: save } = useMutation({
      mutationFn: async ({ editor }: { editor: Editor }) => {
        const heading = editor.$node("heading")?.textContent;
        const collection =
          docDID === did ? "network.habitat.docs" : "network.habitat.docs.edit";
        const mappedKey = docDID === did ? rkey : `${docDID}-${rkey}`;
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: collection,
            rkey: mappedKey,
            record: {
              name: heading ?? "Untitled",
              blob: Y.encodeStateAsUpdateV2(ydoc).toBase64(),
            },
          }),
        );
      },
      onSuccess: () => setDirty(false),
    });
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
    const editor = useEditor({
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
            name: did,
            color: "#f783ac",
          },
        }),
      ],
      onUpdate: handleUpdate,
    });
    return (
      <>
        <article>
          <EditorContent editor={editor} />
        </article>
        {dirty ? "🔄 Syncing" : "✅ Synced"}
        Node id: {node.peerId.toString()}
      </>
    );
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
