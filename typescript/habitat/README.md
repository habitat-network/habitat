# @habitat-network/habitat

TypeScript SDK for building on [Habitat](https://habitat.network), a data
ownership layer for organizations built on [AT Protocol](https://atproto.com)
primitives.

## Install

```bash
npm install @habitat-network/habitat
```

`@atproto-labs/identity-resolver` is a peer dependency. If you are already using
`@atproto/oauth-client` or `@atproto/oauth-client-browser`, you have it.

This package has **no runtime dependencies** — the peer is used for types only.

## Identity resolution

Habitat organizations use `did:web` identities that Habitat itself hosts, so the
stock `AtprotoIdentityResolver` cannot see them. `HabitatIdentityResolver`
resolves through a Habitat instance's `com.atproto.identity.resolveIdentity`
endpoint, which handles both Habitat-hosted and public-network identities.

```ts
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const resolver = new HabitatIdentityResolver();

const { did, handle, didDoc } = await resolver.resolve("acme.habitat.network");
```

It satisfies `IdentityResolver`, so it drops straight into an OAuth client:

```ts
import { BrowserOAuthClient } from "@atproto/oauth-client-browser";
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const client = new BrowserOAuthClient({
  clientMetadata,
  handleResolver: "https://bsky.social",
  identityResolver: new HabitatIdentityResolver(),
});
```

### Self-hosted instances

Pass a service URL to target your own Habitat instance. It defaults to
`https://pear.habitat.network`.

```ts
new HabitatIdentityResolver("https://pear.example.com");
```

### Errors

Every failure — unreachable host, non-2xx XRPC response, malformed payload —
throws `HabitatIdentityResolverError`:

```ts
import { HabitatIdentityResolverError } from "@habitat-network/habitat";

try {
  await resolver.resolve("nobody.example.com");
} catch (error) {
  if (error instanceof HabitatIdentityResolverError) {
    console.error(error.status, error.xrpcError); // 404, "HandleNotFound"
  }
}
```

Aborts are the exception: passing an aborted `signal` rejects with the original
`AbortError`, never wrapped, so cancellation stays distinguishable from failure.

```ts
await resolver.resolve("acme.habitat.network", { signal: controller.signal });
```

## Trust model

Unlike `AtprotoIdentityResolver`, this resolver does **no client-side
bidirectional handle↔DID verification**. It delegates that to the Habitat
instance, which resolves through indigo's directory — that directory verifies
bidirectionally and falls back to public-network resolution for identities
Habitat does not host.

Pointing `serviceUrl` at a host you do not trust therefore means trusting that
host's identity claims outright.

## License

Apache-2.0
