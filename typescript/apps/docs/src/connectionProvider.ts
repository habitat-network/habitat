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

  constructor(node: Node, doc: Y.Doc) {
    super();
    this.node = node;
    this.doc = doc;
    this.awareness = new awarenessProtocol.Awareness(doc);
    this._handleDocUpdate = (update, origin) => {
      if (origin !== this) {
        const encoder = encoding.createEncoder();
        encoding.writeVarUint(encoder, messageTypes.sync);
        syncProtocol.writeUpdate(encoder, update);
        this.node.services.pubsub.publish(
          "test",
          encoding.toUint8Array(encoder),
        );
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
      node.services.pubsub.publish("test", encoding.toUint8Array(encoder));
    };

    node.services.pubsub.addEventListener("message", (message) => {
      const decoder = decoding.createDecoder(message.detail.data);
      const messageType = decoding.readVarUint(decoder);
      switch (messageType) {
        case messageTypes.sync: {
          const encoder = encoding.createEncoder();
          encoding.writeUint8(encoder, messageTypes.sync);
          syncProtocol.readSyncMessage(decoder, encoder, doc, this);
          if (encoding.length(encoder) > 1) {
            node.services.pubsub.publish(
              "test",
              encoding.toUint8Array(encoder),
            );
          }
          return;
        }
        case messageTypes.awareness: {
          const encoder = encoding.createEncoder();
          encoding.writeUint8(encoder, messageTypes.awareness);
          awarenessProtocol.applyAwarenessUpdate(
            this.awareness,
            decoding.readVarUint8Array(decoder),
            this,
          );
          if (encoding.length(encoder) > 1) {
            node.services.pubsub.publish(
              "test",
              encoding.toUint8Array(encoder),
            );
          }
          return;
        }
      }
    });

    node.services.pubsub.addEventListener("gossipsub:graft", () => {
      const encoder = encoding.createEncoder();
      encoding.writeVarUint(encoder, messageTypes.sync);
      syncProtocol.writeSyncStep1(encoder, doc);
      node.services.pubsub.publish("test", encoding.toUint8Array(encoder));

      doc.on("update", this._handleDocUpdate);
      this.awareness.on("update", this._awarenessUpdateHandler);
    });

    node.services.pubsub.addEventListener(
      "subscription-change",
      ({ detail }) => {
        const subscription = detail.subscriptions.find(
          (sub) => sub.topic === "test",
        );
        if (!subscription?.subscribe) {
          doc.off("update", this._handleDocUpdate);
          this.awareness.off("update", this._awarenessUpdateHandler);
        }
      },
    );

    node.services.pubsub.subscribe("test");
  }

  destroy(): void {
    this.node.services.pubsub.unsubscribe("test");
    this.awareness.off("update", this._awarenessUpdateHandler);
    this.doc.off("update", this._handleDocUpdate);
  }
}
