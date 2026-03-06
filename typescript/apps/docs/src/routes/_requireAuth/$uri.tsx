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
import { dcutr } from '@libp2p/dcutr'
import { webRTC } from '@libp2p/webrtc'
import { webTransport } from '@libp2p/webtransport'
import { peerIdFromString } from "@libp2p/peer-id";

const habitatDID = "did:plc:ss2uhsajrstfhkq73fteu4zz"

class ForbiddenError extends Error {
  constructor(public forbiddenHandle: string) { super("forbidden"); }
}

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    // Fetch the record
    const { uri } = params;
    const [, , docDID, lexicon, rkey] = uri.split("/");
    const originalRecordResponse = await context.authManager.fetch(
      `/xrpc/network.habitat.getRecord?repo=${docDID}&collection=${lexicon}&rkey=${rkey}&includePermissions=true`,
    );
    const did = context.authManager.getAuthInfo()!.did;
    const profile = await context.authManager.client().getSelfProfile()

    if (originalRecordResponse?.status === 403) {
      throw new ForbiddenError(profile.handle);
    }

    // The gossipsub topic is also used as the per-document rendezvous key.
    const habitatUri = `habitat://${docDID}/network.habitat.docs/${rkey}`;

    // TODO: this should look up the habitat service on the doc owner's DID and use that endpoint, not the generic __HABITAT_DOMAIN__. This is left for later.
    // All user pear nodes are expected to implement the relay address.
    const domain = __HABITAT_DOMAIN__;
    const relayAddr = multiaddr(`/dns4/${domain}/tcp/443/wss`);
    // setup libp2p
    const node = await createLibp2p({
      addresses: {
        listen: [
          '/p2p-circuit',
          '/webrtc',
        ],
      },
      transports: [
        webRTC({
          rtcConfiguration: {
            iceServers: [
              { urls: ['stun:stun.l.google.com:19302'] }
            ]
          }
        }),
        webTransport(),
        webSockets(),
        circuitRelayTransport()
      ],
      connectionEncrypters: [noise()],
      streamMuxers: [yamux()],
      services: {
        identify: identify(),
        dcutr: dcutr(),
        pubsub: gossipsub({
          runOnLimitedConnection: true,
          allowPublishToZeroTopicPeers: true,
          emitSelf: false,
        }),
      },
    });

    const conn = await node.dial(relayAddr);
    let relayPeerId = conn.remotePeer.toString();

    node.addEventListener("peer:connect", (event) => {
      const peerId = event.detail;
      const isRelay = node.getConnections(peerId)
        .some((c) => c.remoteAddr.toString().includes(domain));
      if (isRelay) {
        relayPeerId = peerId.toString();
      }
    });

    async function dialPeer(peerIdStr: string): Promise<void> {
      if (peerIdStr === node.peerId.toString()) return;
      if (peerIdStr === relayPeerId) return;
      if (node.getConnections(peerIdFromString(peerIdStr)).length > 0) return;
      const circuitAddr = multiaddr(`/p2p/${relayPeerId}/p2p-circuit/p2p/${peerIdStr}`);
      try { await node.dial(circuitAddr); }
      catch (e) { console.log("caught error dialing", e, peerIdStr); }
    }

    node.addEventListener('connection:open', (evt) => {
      const conn = evt.detail
      const addr = conn.remoteAddr.toString()

      const isDirect = addr.includes('/webrtc') && !addr.includes('p2p-circuit')
      const isRelayed = addr.includes('p2p-circuit')

      console.log(`connection to ${conn.remotePeer}: ${isDirect ? 'direct WebRTC' : isRelayed ? 'relayed' : 'websocket'}`)
    })


    async function getServiceAuthToken(): Promise<string> {
      const response = await context.authManager.fetch(
        // TODO: lxm is a random atproto lexicon right now because serviceAuth endpoint only accepts valid published lexicons
        // We need to publish network.habitat.p2p and pass that in here.
        `/xrpc/com.atproto.server.getServiceAuth?aud=${encodeURIComponent(habitatDID)}&lxm=com.atproto.server.getServiceAuth`,
      );
      const data = await response?.json();
      return data.token;
    }


    async function startPeerDiscovery(): Promise<void> {
      try {
        const stream = await node.dialProtocol(
          peerIdFromString(relayPeerId), "/habitat/peer-discovery/1.0.0");
        const oauthToken = context.authManager.getAuthInfo()?.accessToken ?? "";
        const serviceAuthToken = await getServiceAuthToken()

        const encoder = new TextEncoder();
        stream.sink((async function* () {
          yield encoder.encode(JSON.stringify({ topic: habitatUri, oauth_token: oauthToken, serviceauth_token: serviceAuthToken }));
        })());

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
              console.log("error dialing peer: ", e)
            });
          }
        }
      } catch {

      }
    }

    startPeerDiscovery().catch(() => { });

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

    async function refetchDoc() {
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
    }

    async function mergeOtherEdits() {
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

    }

    async function writeChanges() {
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

    async function dialRelay(): Promise<string> {
      const conn = await node.dial(relayAddr);
      return conn.remotePeer.toString();
    }

    function onVisibilityChange() {
      if (document.visibilityState === "visible") {
        dialRelay().then((p) => { relayPeerId = p; })
          .then(() => startPeerDiscovery())
          .then(refetchDoc)
          .then(mergeOtherEdits)
          .then(writeChanges)
          .catch(() => { });
      }
    }
    document.addEventListener("visibilitychange", onVisibilityChange);

    const provider = new Libp2pConnectionProvider(node, ydoc, habitatUri);

    return {
      provider,
      node,
      ydoc,
      rkey,
      profile,
      docDID: docDID,
      onVisibilityChange,
    };
  },
  onLeave({ loaderData }) {
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
    const { profile, docDID, rkey, ydoc, provider } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const [dirty, setDirty] = useState(false);

    const { mutate: save } = useMutation({
      mutationFn: async ({ editor }: { editor: Editor }) => {
        const heading = editor.$node("heading")?.textContent;
        const collection =
          docDID === profile.did ? "network.habitat.docs" : "network.habitat.docs.edit";
        const mappedKey = docDID === profile.did ? rkey : `${docDID}-${rkey}`;
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: profile.did,
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
            name: profile.handle,
            color: "#f783ac",
          },
        }),
      ],
      onUpdate: handleUpdate,
    });
    return (
      <>
        <a href="/">My docs</a>
        <p>Logged in as: @{profile.handle}</p>
        <article>
          <EditorContent editor={editor} />
        </article>
        {dirty ? "🔄 Syncing" : "✅ Synced"}
      </>
    );
  },
  errorComponent({ error }) {
    if (error instanceof ForbiddenError) {
      return <>
        <p>Logged in as: @{error.forbiddenHandle}</p>
        <p>You do not have permission to view this doc</p>
      </>;
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
