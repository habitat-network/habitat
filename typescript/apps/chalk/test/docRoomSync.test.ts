import { expect, it } from "vitest";
import * as Y from "yjs";
import * as encoding from "lib0/encoding";
import * as decoding from "lib0/decoding";
import * as syncProtocol from "y-protocols/sync";
import { syncReplies } from "../src/server/rooms/docRoom";

// These exercise the sync handshake as pure functions rather than over a real
// socket, for the harness reason documented at the top of
// docRoomSubscribe.test.ts: vitest-pool-workers does not deliver message
// bytes sent from inside a Durable Object, so "what does the room send back"
// is not observable through stub.fetch(). syncReplies is the whole of that
// decision, so testing it directly tests the behavior that matters.

const MESSAGE_SYNC = 0;

function step1(doc: Y.Doc): Uint8Array {
  const encoder = encoding.createEncoder();
  encoding.writeVarUint(encoder, MESSAGE_SYNC);
  syncProtocol.writeSyncStep1(encoder, doc);
  return encoding.toUint8Array(encoder);
}

function innerType(message: Uint8Array): number {
  const decoder = decoding.createDecoder(message);
  decoding.readVarUint(decoder); // outer messageSync byte
  return decoding.readVarUint(decoder);
}

// The client half of the handshake, done exactly the way y-websocket's own
// WebsocketProvider does it (see its messageHandlers[messageSync]): feed the
// message to readSyncMessage and send back whatever it wrote to the encoder.
function clientReply(message: Uint8Array, doc: Y.Doc): Uint8Array | undefined {
  const decoder = decoding.createDecoder(message);
  decoding.readVarUint(decoder); // outer messageSync byte
  const encoder = encoding.createEncoder();
  encoding.writeVarUint(encoder, MESSAGE_SYNC);
  syncProtocol.readSyncMessage(decoder, encoder, doc, "client");
  return encoding.length(encoder) > 1
    ? encoding.toUint8Array(encoder)
    : undefined;
}

it("answers a client's sync step 1 with its own state", () => {
  const room = new Y.Doc();
  room.getText("body").insert(0, "hello");
  const replies = syncReplies(step1(new Y.Doc()), room, null);
  expect(replies.map(innerType)).toContain(syncProtocol.messageYjsSyncStep2);
});

it("also asks the client for what the room is missing", () => {
  const room = new Y.Doc();
  const replies = syncReplies(step1(new Y.Doc()), room, null);
  expect(replies.map(innerType)).toContain(syncProtocol.messageYjsSyncStep1);
});

it("does not ask again when answering a client's sync step 2", () => {
  const room = new Y.Doc();
  const client = new Y.Doc();
  client.getText("body").insert(0, "hi");
  const step2 = clientReply(step1(room), client)!;
  expect(syncReplies(step2, room, null)).toHaveLength(0);
});

it("recovers an update the room never received", () => {
  // The real failure: a client makes three edits and the room misses the
  // middle one, so every later edit from that client references a struct the
  // room does not have. Yjs parks them in pendingStructs and the room's
  // document is frozen at the last complete update — which is what renderDoc
  // then publishes as the doc's title.
  const client = new Y.Doc();
  const updates: Uint8Array[] = [];
  client.on("update", (u: Uint8Array) => updates.push(u));
  const text = client.getText("body");
  text.insert(0, "pace host");
  client.transact(() => text.insert(0, "S"));
  client.transact(() => text.insert(text.length, " as a service"));

  const room = new Y.Doc();
  updates.forEach((u, i) => {
    if (i !== 1) Y.applyUpdate(room, u);
  });
  expect(room.getText("body").toString()).toBe("pace host");
  expect(room.store.pendingStructs).not.toBeNull();

  // The client opens the connection with sync step 1, as WebsocketProvider
  // does on every connect and reconnect, and answers whatever the room asks.
  for (const reply of syncReplies(step1(client), room, null)) {
    const back = clientReply(reply, client);
    if (back) syncReplies(back, room, null);
  }

  expect(room.getText("body").toString()).toBe("Space host as a service");
});
