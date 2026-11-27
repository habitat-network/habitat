import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { appIndexer } from "@/server/stream/appIndexer";

describe("appIndexer", () => {
  it("asks the stream for exactly the collections the app indexes", () => {
    expect(appIndexer(env).collections()?.sort()).toEqual([
      "group.opensocial.membership",
      "network.habitat.apps.app",
      "network.habitat.docs.comment",
      "network.habitat.docs.commentReply",
      "network.habitat.docs.crdt",
      "network.habitat.docs.markdown",
      "network.habitat.drop.file",
      "network.habitat.relationship.spaceRelation",
      "network.habitat.relationship.userRelation",
    ]);
  });
});
