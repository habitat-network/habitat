# @habitat-network/habitat

TypeScript SDK for building on [Habitat](https://habitat.network), a data
ownership layer for organizations built on [AT Protocol](https://atproto.com)
primitives.

## Install

```bash
npm install @habitat-network/habitat
```

## Identity resolution

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
  identityResolver: new HabitatIdentityResolver(),
});
```

### Self-hosted instances

Pass a service URL to target your own Habitat instance. It defaults to
`https://pear.habitat.network`.

```ts
new HabitatIdentityResolver("https://pear.example.com");
```

## License

Apache-2.0
