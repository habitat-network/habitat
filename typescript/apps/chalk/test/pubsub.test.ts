import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { DocPubSub } from "../src/server/pubsub";

describe("DocPubSub", () => {
  it("delivers published docs to an active subscriber", async () => {
    const pubsub = new DocPubSub();
    const received: string[] = [];

    const consume = (async () => {
      for await (const doc of pubsub.subscribe("space1")) {
        received.push(doc.getText("t").toString());
        if (received.length === 2) break;
      }
    })();

    const doc1 = new Y.Doc();
    doc1.getText("t").insert(0, "first");
    pubsub.publish("space1", doc1);

    const doc2 = new Y.Doc();
    doc2.getText("t").insert(0, "second");
    pubsub.publish("space1", doc2);

    await consume;
    expect(received).toEqual(["first", "second"]);
  });

  it("does not deliver to subscribers of a different space", async () => {
    const pubsub = new DocPubSub();
    const received: string[] = [];
    const consume = (async () => {
      for await (const doc of pubsub.subscribe("space1")) {
        received.push(doc.getText("t").toString());
      }
    })();

    const other = new Y.Doc();
    other.getText("t").insert(0, "ignored");
    pubsub.publish("space2", other);

    const mine = new Y.Doc();
    mine.getText("t").insert(0, "mine");
    pubsub.publish("space1", mine);

    // Give the async generator a tick to receive "mine" before we stop it.
    await new Promise((r) => setTimeout(r, 0));
    expect(received).toEqual(["mine"]);
    void consume;
  });
});
