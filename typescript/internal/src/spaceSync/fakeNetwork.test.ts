// @vitest-environment node
import { P256Keypair } from "@atproto/crypto";
import { createSpaceSigHeaders, readRepoStar } from "@atproto/space";
import type { DidString } from "@atproto/syntax";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import { FakeNetwork } from "./test/fakeNetwork";

describe("FakeNetwork", () => {
  it("issues a credential and serves a readable repo STAR archive", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const bob = await net.createAccount("bob");
    const space = net.createSpace(alice);
    await space.write(bob, "com.example.post", "a", { text: "hi" });
    await space.write(bob, "com.example.post", "b", { text: "yo" });
    await space.write(bob, "com.example.post", "a", null);

    const key = await P256Keypair.create();
    const delegation = await net.delegationToken(alice, space.id);
    const credRes = await fetch(
      `${alice.pds}/xrpc/com.atproto.space.getSpaceCredential`,
      {
        method: "POST",
        headers: {
          ...(await createSpaceSigHeaders(key, {
            authorization: `Bearer ${delegation}`,
          })),
          "content-type": "application/json",
        },
        body: JSON.stringify({ space: space.id }),
      },
    );
    expect(credRes.status).toBe(200);
    const { credential } = (await credRes.json()) as { credential: string };

    const url = new URL(`${bob.pds}/xrpc/com.atproto.space.getRepo`);
    url.searchParams.set("space", space.id);
    url.searchParams.set("repo", bob.did);
    const repoRes = await fetch(url, {
      headers: await createSpaceSigHeaders(key, {
        authorization: `Atproto-Space ${credential}`,
        audience: bob.did as DidString,
      }),
    });
    expect(repoRes.status).toBe(200);
    expect(repoRes.headers.get("content-type")).toBe(
      "application/octet-stream",
    );
    // TODO(shared-secret MACs): verify the commit with verifyRepoStarFull.
    await using repo = await readRepoStar([
      new Uint8Array(await repoRes.arrayBuffer()),
    ]);
    const rkeys: string[] = [];
    for await (const record of repo.records) rkeys.push(record.rkey);
    expect(rkeys).toEqual(["b"]);
    expect(repo.commit.rev).toBe(space.repoRevOf(bob.did));
  });

  it("rejects repo calls signed for the wrong audience", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const space = net.createSpace(alice);
    await space.write(alice, "com.example.post", "a", { text: "hi" });
    const key = await P256Keypair.create();
    const delegation = await net.delegationToken(alice, space.id);
    const { credential } = (await (
      await fetch(`${alice.pds}/xrpc/com.atproto.space.getSpaceCredential`, {
        method: "POST",
        headers: {
          ...(await createSpaceSigHeaders(key, {
            authorization: `Bearer ${delegation}`,
          })),
          "content-type": "application/json",
        },
        body: JSON.stringify({ space: space.id }),
      })
    ).json()) as { credential: string };
    const url = new URL(`${alice.pds}/xrpc/com.atproto.space.listRepoOps`);
    url.searchParams.set("space", space.id);
    url.searchParams.set("repo", alice.did);
    const res = await fetch(url, {
      headers: await createSpaceSigHeaders(key, {
        authorization: `Atproto-Space ${credential}`,
        audience: "did:plc:someoneelseaaaaaaaaaaaaa" as DidString,
      }),
    });
    expect(res.status).toBe(401);
  });
});
