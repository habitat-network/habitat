import { Cause, Effect, Queue, Stream } from "effect";
import { ConnectionError, errorMessage } from "./errors";

/**
 * An authenticated fetch: an OAuth session's fetchHandler, or anything else
 * that adds the caller's credentials. Given absolute URLs.
 */
export type FetchHandler = (
  url: string,
  init?: RequestInit,
) => Promise<Response>;

/**
 * How the stream's WebSocket is opened. Each run of the returned stream is
 * one connection: its text frames, ending (with a ConnectionError) when the
 * socket closes, and closing the socket when the stream is interrupted.
 */
export type Transport = (url: URL) => Stream.Stream<string, ConnectionError>;

/** The parts of a WebSocket the transports use. */
export interface SocketLike {
  addEventListener(type: "message", fn: (e: { data: unknown }) => void): void;
  addEventListener(
    type: "close",
    fn: (e: { code: number; reason: string }) => void,
  ): void;
  addEventListener(type: "error", fn: (e: unknown) => void): void;
  addEventListener(type: "open", fn: () => void): void;
  close(code?: number, reason?: string): void;
}

const decoder = new TextDecoder();

function textOf(data: unknown): string | undefined {
  if (typeof data === "string") return data;
  if (data instanceof ArrayBuffer) return decoder.decode(data);
  if (ArrayBuffer.isView(data)) return decoder.decode(data);
  return undefined;
}

// pipeSocket feeds socket's text frames into queue until it closes.
function pipeSocket(
  socket: SocketLike,
  queue: Queue.Queue<string, ConnectionError | Cause.Done>,
) {
  socket.addEventListener("message", (e) => {
    const text = textOf(e.data);
    if (text !== undefined) Queue.offerUnsafe(queue, text);
  });
  socket.addEventListener("close", (e) => {
    Queue.failCauseUnsafe(
      queue,
      Cause.fail(
        new ConnectionError({
          message: `stream closed (${e.code}${e.reason ? `: ${e.reason}` : ""})`,
        }),
      ),
    );
  });
  socket.addEventListener("error", (e) => {
    Queue.failCauseUnsafe(
      queue,
      Cause.fail(
        new ConnectionError({ message: `stream error: ${errorMessage(e)}` }),
      ),
    );
  });
}

/**
 * fetchTransport opens the WebSocket as an HTTP upgrade through fetch, the
 * way Cloudflare Workers make outbound WebSockets: the request goes through
 * the authenticated fetch handler, so the org's OAuth (DPoP) credentials
 * ride on the upgrade request like on any other call.
 */
export const fetchTransport =
  (fetch: FetchHandler): Transport =>
  (url) =>
    Stream.callback<string, ConnectionError>((queue) =>
      Effect.gen(function* () {
        const httpUrl = new URL(url);
        httpUrl.protocol = url.protocol === "ws:" ? "http:" : "https:";
        const res = yield* Effect.tryPromise({
          try: () =>
            fetch(httpUrl.toString(), { headers: { Upgrade: "websocket" } }),
          catch: (cause) =>
            new ConnectionError({ message: `connect: ${errorMessage(cause)}` }),
        });
        const socket = (
          res as { webSocket?: (SocketLike & { accept(): void }) | null }
        ).webSocket;
        if (res.status !== 101 || !socket) {
          const body = yield* Effect.promise(() => res.text().catch(() => ""));
          return yield* new ConnectionError({
            message: `upgrade refused (${res.status}): ${body}`,
            status: res.status,
          });
        }
        socket.accept();
        pipeSocket(socket, queue);
        yield* Effect.addFinalizer(() =>
          Effect.sync(() => {
            try {
              socket.close(1000, "done");
            } catch {
              // already closed
            }
          }),
        );
      }),
    );

/**
 * webSocketTransport opens the WebSocket with a constructor, for runtimes
 * with a standard WebSocket (Node, browsers). Credentials have to be added
 * by `open` itself (e.g. the `ws` package's headers option): the standard
 * constructor can't send an Authorization header.
 */
export const webSocketTransport =
  (open: (url: URL) => SocketLike): Transport =>
  (url) =>
    Stream.callback<string, ConnectionError>((queue) =>
      Effect.gen(function* () {
        const socket = yield* Effect.try({
          try: () => open(url),
          catch: (cause) =>
            new ConnectionError({ message: `connect: ${errorMessage(cause)}` }),
        });
        pipeSocket(socket, queue);
        yield* Effect.addFinalizer(() =>
          Effect.sync(() => {
            try {
              socket.close(1000, "done");
            } catch {
              // already closed
            }
          }),
        );
      }),
    );
