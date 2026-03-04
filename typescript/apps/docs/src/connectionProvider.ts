import { ObservableV2 } from "lib0/observable";
import { Libp2p } from "libp2p";
import { gossipsub } from "@chainsafe/libp2p-gossipsub";
import * as Y from "yjs";
import * as syncProtocol from "y-protocols/sync";
import * as awarenessProtocol from "y-protocols/awareness";
import * as encoding from "lib0/encoding";
import * as decoding from "lib0/decoding";

const messageTypes = {
  sync: 0,
  awareness: 1,
} as const;

type Node = Libp2p<{ pubsub: ReturnType<ReturnType<typeof gossipsub>> }>;

export class Libp2pConnectionProvider extends ObservableV2<{
  "connection-close": (
    event: CloseEvent | null,
    provider: Libp2pConnectionProvider,
  ) => any;
  status: (event: {
    status: "connected" | "disconnected" | "connecting";
  }) => any;
  "connection-error": (event: Event, provider: Libp2pConnectionProvider) => any;
  sync: (state: boolean) => any;
}> {
  node: Node;
  doc: Y.Doc;
  awareness: awarenessProtocol.Awareness;

  _handleDocUpdate: (update: Uint8Array, origin: any) => void;
  _awarenessUpdateHandler: (
    update: {
      added: number[];
      updated: number[];
      removed: number[];
    },
    origin: any,
  ) => void;

  topic: string;

  constructor(node: Node, doc: Y.Doc, topic: string) {
    super();
    this.node = node;
    this.doc = doc;
    this.topic = topic;
    this.awareness = new awarenessProtocol.Awareness(doc);
    this._handleDocUpdate = (update, origin) => {
      if (origin !== this) {
        const encoder = encoding.createEncoder();
        encoding.writeVarUint(encoder, messageTypes.sync);
        syncProtocol.writeUpdate(encoder, update);
        this.node.services.pubsub
          .publish(this.topic, encoding.toUint8Array(encoder))
          .catch((err: Error) => {
            console.error("[pubsub] publish error", err);
          });
      }
    };

    this._awarenessUpdateHandler = ({ added, updated, removed }, _origin) => {
      const changedClients = added.concat(updated).concat(removed);
      const encoder = encoding.createEncoder();
      encoding.writeVarUint(encoder, messageTypes.awareness);
      encoding.writeVarUint8Array(
        encoder,
        awarenessProtocol.encodeAwarenessUpdate(this.awareness, changedClients),
      );

      node.services.pubsub
        .publish(this.topic, encoding.toUint8Array(encoder))
        .catch((err: Error) => {
          console.error("[pubsub] publish error", err);
        });
    };

    node.services.pubsub.addEventListener("message", (message) => {
      if (message.detail.topic !== this.topic) return;
      const decoder = decoding.createDecoder(message.detail.data);
      const messageType = decoding.readVarUint(decoder);
      switch (messageType) {
        case messageTypes.sync: {
          const encoder = encoding.createEncoder();
          encoding.writeVarUint(encoder, messageTypes.sync);
          syncProtocol.readSyncMessage(decoder, encoder, doc, this);
          if (encoding.length(encoder) > 1) {
            node.services.pubsub
              .publish(this.topic, encoding.toUint8Array(encoder))
              .catch((err: Error) => {
                console.error("[pubsub] publish error", err);
              });
          }
          return;
        }
        case messageTypes.awareness: {
          awarenessProtocol.applyAwarenessUpdate(
            this.awareness,
            decoding.readVarUint8Array(decoder),
            this,
          );
          return;
        }
      }
    });

    // Send sync step 1 when a peer joins the mesh for this specific topic.
    // gossipsub:graft fires after the mesh is established, so publish is guaranteed
    // to have at least one recipient. The topic filter prevents spurious syncs for
    // unrelated gossipsub-internal topics.
    node.services.pubsub.addEventListener("gossipsub:graft", (event) => {
      const { topic } = (
        event as CustomEvent<{ peerId: unknown; topic: string }>
      ).detail;
      if (topic !== this.topic) return;
      const encoder = encoding.createEncoder();
      encoding.writeVarUint(encoder, messageTypes.sync);
      syncProtocol.writeSyncStep1(encoder, doc);
      node.services.pubsub
        .publish(this.topic, encoding.toUint8Array(encoder))
        .catch((err: Error) => {
          console.error("[pubsub] publish error", err);
        });
    });

    // Attach update handlers immediately so local changes are captured from the
    // start. Publishes before mesh formation will be dropped silently, but the
    // sync step 1 exchange on graft ensures both peers converge on the full state.
    doc.on("update", this._handleDocUpdate);
    this.awareness.on("update", this._awarenessUpdateHandler);

    console.log("subscribing to", this.topic);
    node.services.pubsub.subscribe(this.topic);
  }

  destroy(): void {
    this.node.services.pubsub.unsubscribe(this.topic);
    this.awareness.off("update", this._awarenessUpdateHandler);
    this.doc.off("update", this._handleDocUpdate);
  }
}
