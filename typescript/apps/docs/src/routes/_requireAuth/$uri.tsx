import { Editor, EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { createLibp2p } from "libp2p";
import { webSockets } from "@libp2p/websockets";
import { circuitRelayTransport } from '@libp2p/circuit-relay-v2'
import { multiaddr } from "@multiformats/multiaddr";
import { noise } from "@chainsafe/libp2p-noise";
import { yamux } from "@chainsafe/libp2p-yamux";
import { identify } from "@libp2p/identify";
import { gossipsub } from "@chainsafe/libp2p-gossipsub";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { Libp2pConnectionProvider } from "@/connectionProvider";
import { bootstrap } from '@libp2p/bootstrap'
import { peerIdFromString } from '@libp2p/peer-id'


// const DISCOVERY_PROTOCOL = "/habitat/peer-discovery/1.0.0";

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const relayAddr = multiaddr(`/dns4/${__HABITAT_DOMAIN__}/tcp/443/wss`)
    // setup libp2p
    const node = await createLibp2p({
      addresses: {
        listen: ['/p2p-circuit'],  // ADD THIS
      },
      transports: [
        webSockets(),
        circuitRelayTransport(),
      ],
      peerDiscovery: [
        bootstrap({
          list: [relayAddr.toString()]
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
    console.log("addr", `/dns4/${__HABITAT_DOMAIN__}/tcp/443/wss`)
    console.log("multiaddr1 ", multiaddr(`/dns4/${__HABITAT_DOMAIN__}/tcp/443/wss`))
    const conn = await node.dial(relayAddr);
    console.log("my peer id", node.peerId.toString())
    const relayPeerId = conn.remotePeer.toString();
    console.log(`Connected to habitat relay ${relayPeerId}`);

    // fetch original record
    const { uri } = params;
    const [, , docDID, lexicon, rkey] = uri.split("/");
    const originalRecordResponse = await context.authManager.fetch(
      `/xrpc/network.habitat.getRecord?repo=${docDID}&collection=${lexicon}&rkey=${rkey}`,
    );

    // The gossipsub topic is also used as the per-document rendezvous key.
    const habitatUri = `habitat://${docDID}/network.habitat.docs/${rkey}`;

    // Register with the relay so other browsers editing this document can find us.
    await fetch(`https://${__HABITAT_DOMAIN__}/p2p/peers`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peerId: node.peerId.toString(), topic: habitatUri }),
    });


    async function discoverAndDialPeers(): Promise<void> {
      console.log("discovering peers")
      let peerIds: string[];
      try {
        const res = await fetch(
          `https://${__HABITAT_DOMAIN__}/p2p/peers?topic=${encodeURIComponent(habitatUri)}`
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
        if (node.getConnections(peerIdFromString(peerIdStr)).length > 0) continue;
        console.log("found new peer", peerIdStr)
        const circuitAddr = multiaddr(
          `/p2p/${relayPeerId}/p2p-circuit/p2p/${peerIdStr}`
        );
        try {
          await node.dial(circuitAddr);
          console.log(`[peer-discovery] connected to ${peerIdStr} via circuit relay`);
        } catch (e) {
          console.log("caught error dialing", e, peerIdStr)
          // reservation not ready or peer left — retry next tick
        }
      }
    }

    await discoverAndDialPeers();
    const discoveryInterval = window.setInterval(discoverAndDialPeers, 500 /* 500ms for testing */);

    const data: {
      uri: string;
      cid: string;
      value: HabitatDoc;
    } = await originalRecordResponse?.json();

    const ydoc = new Y.Doc();
    if (data.value.blob) {
      Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(data.value.blob));
    }

    const did = context.authManager.getAuthInfo()?.did;

    if (docDID !== did) {
      const editsRecordResponse = await context.authManager.fetch(
        `/xrpc/network.habitat.getRecord?repo=${did}&collection=network.habitat.docs.edit&rkey=${rkey}`,
      );
      try {
        const data: {
          uri: string;
          cid: string;
          value: HabitatDoc;
        } = await editsRecordResponse?.json();
        if (data.value.blob) {
          Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(data.value.blob));
        }
      } catch { }
    }

    const provider = new Libp2pConnectionProvider(node, ydoc, habitatUri);

    return {
      provider,
      node,
      ydoc,
      rkey,
      did: did,
      docDID: docDID,
      discoveryInterval,
    };
  },
  onLeave({ loaderData }) {
    console.log("on leave");
    if (loaderData?.discoveryInterval !== undefined) {
      window.clearInterval(loaderData.discoveryInterval);
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
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection:
              docDID === did
                ? "network.habitat.docs"
                : "network.habitat.docs.edit",
            rkey,
            record: {
              name: heading ?? "Untitled",
              blob: Y.encodeStateAsUpdateV2(ydoc).toBase64(),
            },
          }),
        );
        if (docDID !== did) {
          await authManager.fetch(
            "/xrpc/network.habitat.putRecord",
            "POST",
            JSON.stringify({
              repo: did,
              collection: "network.habitat.docs.edit",
              record: {
                did: docDID,
                originDid: did,
                collection: "network.habitat.docs",
                rkey,
              },
            }),
          );
        }
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