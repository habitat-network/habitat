import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import type { PingRoom } from "../src/server/rooms/ping";

it("persists state across calls to the same id", async () => {
  const id = env.PING.idFromName("a");
  const stub = env.PING.get(id);
  await runInDurableObject(stub, (room: PingRoom) => room.bump());
  const second = await runInDurableObject(stub, (room: PingRoom) => room.bump());
  expect(second).toBe(2);
});

it("gives different ids independent state", async () => {
  const other = env.PING.get(env.PING.idFromName("b"));
  const n = await runInDurableObject(other, (room: PingRoom) => room.bump());
  expect(n).toBe(1);
});
