import { env, runInDurableObject } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import type { SapChannel } from "../src/server/rooms/sapChannel";

const fetchMock = vi.fn();
beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

function fakeSocket() {
  return {
    readyState: WebSocket.OPEN,
    accept: vi.fn(),
    addEventListener: vi.fn(),
    send: vi.fn(),
  } as unknown as WebSocket;
}

it("connects once and no-ops while the socket is open", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  // 200, not 101: the Fetch spec's Response constructor rejects
  // non-2xx-except-network-status codes, and ensureConnected only checks
  // `res.webSocket`, not the status.
  const res = new Response(null, { status: 200 });
  Object.defineProperty(res, "webSocket", { value: fakeSocket() });
  fetchMock.mockResolvedValue(res);
  await runInDurableObject(stub, async (c: SapChannel) => {
    await c.ensureConnected();
    await c.ensureConnected();
  });
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it("targets sap's /channel over ws", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("lifecycle-2"));
  // 200, not 101: the Fetch spec's Response constructor rejects
  // non-2xx-except-network-status codes, and ensureConnected only checks
  // `res.webSocket`, not the status.
  const res = new Response(null, { status: 200 });
  Object.defineProperty(res, "webSocket", { value: fakeSocket() });
  fetchMock.mockResolvedValue(res);
  await runInDurableObject(stub, (c: SapChannel) => c.ensureConnected());
  expect(String(fetchMock.mock.calls[0]![0])).toMatch(/^ws:\/\/.*\/channel$/);
});
