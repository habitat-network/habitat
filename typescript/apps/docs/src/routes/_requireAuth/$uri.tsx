import { Editor, EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { createLibp2p } from "libp2p";
import { webSockets } from "@libp2p/websockets";
import { multiaddr } from "@multiformats/multiaddr";
import { noise } from "@chainsafe/libp2p-noise";
import { yamux } from "@chainsafe/libp2p-yamux";
import { identify } from "@libp2p/identify";
import { gossipsub } from "@chainsafe/libp2p-gossipsub";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { Libp2pConnectionProvider } from "@/connectionProvider";

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    // setup libp2p
    const node = await createLibp2p({
      transports: [webSockets()],
      connectionEncrypters: [noise()],
      streamMuxers: [yamux()],
      services: {
        identify: identify(),
        pubsub: gossipsub(),
      },
    });
    const conn = await node.dial(
      multiaddr(`/dns4/${__HABITAT_DOMAIN__}/tcp/443/wss`),
    );
    console.log(`Connected to habitat node ${conn.remotePeer.toString()}`);

    // fetch original record
    const { uri } = params;
    const [, , did, lexicon, rkey] = uri.split("/");
    const originalRecordResponse = await context.authManager.fetch(
      `/xrpc/network.habitat.getRecord?repo=${did}&collection=${lexicon}&rkey=${rkey}`,
    );

    const data: {
      uri: string;
      cid: string;
      value: HabitatDoc;
    } = await originalRecordResponse?.json();

    const ydoc = new Y.Doc();
    if (data.value.blob) {
      Y.applyUpdateV2(ydoc, Uint8Array.fromBase64(data.value.blob));
    }

    if (did !== context.authManager.handle) {
      const editsRecordResponse = await context.authManager.fetch(
        `/xrpc/network.habitat.getRecord?repo=${context.authManager.handle}&collection=com.habitat.docs.edit&rkey=${rkey}`,
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
      } catch {}
    }

    const provider = new Libp2pConnectionProvider(node, ydoc);

    return {
      provider,
      node,
      ydoc,
      rkey,
      did,
    };
  },
  onLeave({ loaderData }) {
    console.log("on leave");
    loaderData?.provider.destroy();
    loaderData?.ydoc.destroy();
    loaderData?.node.services.pubsub.unsubscribe("test");
    loaderData?.node.stop();
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const { did, rkey, ydoc, provider, node } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const [dirty, setDirty] = useState(false);
    const { mutate: save } = useMutation({
      mutationFn: async ({ editor }: { editor: Editor }) => {
        const heading = editor.$node("heading")?.textContent;
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: authManager.handle,
            collection:
              did === authManager.handle
                ? "com.habitat.docs"
                : "com.habitat.docs.edit",
            rkey,
            record: {
              name: heading ?? "Untitled",
              blob: Y.encodeStateAsUpdateV2(ydoc).toBase64(),
            },
          }),
        );
        if (did !== authManager.handle) {
          await authManager.fetch(
            "/xrpc/network.habitat.notification.createNotification",
            "POST",
            JSON.stringify({
              repo: authManager.handle,
              collection: "com.habitat.docs.edit",
              record: {
                did: did,
                originDid: authManager.handle,
                collection: "com.habitat.docs",
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
            name: "sashank",
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
