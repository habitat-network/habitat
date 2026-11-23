import type { SpaceRefString } from "@atproto/syntax";
import { Context, Effect, Layer } from "effect";
import { SpaceSyncConfig } from "./config";

const encoder = new TextEncoder();

const toBase64Url = (bytes: Uint8Array): string =>
  btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");

/**
 * The opaque token a space host echoes in every notification it sends us about
 * a space. It is `HMAC-SHA256(secret, "atproto-space-notify:" + space)`, so a notification can be
 * authenticated without remembering what we issued: the same token is given on
 * every registration for a space, which the host treats as one registration.
 * The secret is `notifySecret`, or random per process when unset, in which case
 * notifications for registrations made before a restart are rejected until the
 * next credential renews them.
 */
export class NotifyToken extends Context.Service<
  NotifyToken,
  {
    readonly issue: (space: SpaceRefString) => Effect.Effect<string>;
    readonly verify: (
      space: SpaceRefString,
      token: string,
    ) => Effect.Effect<boolean>;
  }
>()("internal/spaceSync/NotifyToken") {
  static readonly layer = Layer.effect(
    NotifyToken,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const secret = config.notifySecret
        ? encoder.encode(config.notifySecret)
        : crypto.getRandomValues(new Uint8Array(32));
      const key = yield* Effect.promise(() =>
        crypto.subtle.importKey(
          "raw",
          secret,
          { name: "HMAC", hash: "SHA-256" },
          false,
          ["sign"],
        ),
      );
      const issue = (space: SpaceRefString) =>
        Effect.promise(async () =>
          toBase64Url(
            new Uint8Array(
              await crypto.subtle.sign(
                "HMAC",
                key,
                encoder.encode(`atproto-space-notify:${space}`),
              ),
            ),
          ),
        );
      return NotifyToken.of({
        issue,
        verify: (space, token) =>
          issue(space).pipe(
            Effect.map((expected) => {
              // Compare every byte so the check doesn't leak how much matched.
              if (expected.length !== token.length) return false;
              let diff = 0;
              for (let i = 0; i < expected.length; i++)
                diff |= expected.charCodeAt(i) ^ token.charCodeAt(i);
              return diff === 0;
            }),
          ),
      });
    }),
  );
}
