/**
 * Accounts on the test bed's alpha PDS (compose.yaml `pds`, the spaces alpha
 * image), used as the identities in tests that sign in for real: they are
 * did:plc identities in the local PLC directory with handles under
 * pds.habitat-e2e.dev, so pear treats them like any member on an external PDS.
 *
 * Seeding is idempotent: an account that already exists (a test bed that is
 * kept running, see PDS_ADDRESS in harness/pear.ts) is signed in to instead of
 * created again. Creating one takes well under a second, so a fresh bed simply
 * seeds them on every run; see harness/seed.ts for why there is no data snapshot.
 */

export const PDS_HANDLE_DOMAIN = "pds.habitat-e2e.dev";

/**
 * Every account here needs its handle listed in the caddy network aliases in
 * compose.yaml, since Docker DNS has no wildcards.
 */
export const PDS_ACCOUNTS = {
  alice: {
    handle: `alice.${PDS_HANDLE_DOMAIN}`,
    password: "alice-e2e-password",
  },
  bob: { handle: `bob.${PDS_HANDLE_DOMAIN}`, password: "bob-e2e-password" },
  // The Habitat app's tests (chalk-docs, apps-deploy) sign in as carol and
  // put her in groups; the web client's tests expect alice and bob in none,
  // since their sign-in lands on the first group they belong to.
  carol: {
    handle: `carol.${PDS_HANDLE_DOMAIN}`,
    password: "carol-e2e-password",
  },
} as const;

export type PdsAccountName = keyof typeof PDS_ACCOUNTS;

export interface PdsAccount {
  handle: string;
  password: string;
  did: string;
}

export type PdsAccounts = Record<PdsAccountName, PdsAccount>;

/** Creates (or finds) every account in PDS_ACCOUNTS on the PDS at host:port `address`. */
export async function seedPdsAccounts(address: string): Promise<PdsAccounts> {
  const base = `http://${address}`;
  const entries = await Promise.all(
    (Object.keys(PDS_ACCOUNTS) as PdsAccountName[]).map(async (name) => {
      const { handle, password } = PDS_ACCOUNTS[name];
      const did =
        (await signIn(base, handle, password)) ??
        (await createAccount(base, handle, password, `${name}@example.com`));
      return [name, { handle, password, did }] as const;
    }),
  );
  return Object.fromEntries(entries) as PdsAccounts;
}

async function signIn(
  base: string,
  identifier: string,
  password: string,
): Promise<string | undefined> {
  const res = await xrpc(base, "com.atproto.server.createSession", {
    identifier,
    password,
  });
  if (res.ok) return ((await res.json()) as { did: string }).did;
  // An unknown account is a 401 AuthenticationRequired; anything else is a
  // PDS that is not working.
  if (res.status !== 401)
    throw new Error(`createSession ${res.status}: ${await res.text()}`);
  return undefined;
}

async function createAccount(
  base: string,
  handle: string,
  password: string,
  email: string,
): Promise<string> {
  // The PDS reports healthy before the PLC directory (which account creation
  // writes to) has finished migrating, so retry server errors for a while.
  const deadline = Date.now() + 60_000;
  let res: Response;
  for (;;) {
    res = await xrpc(base, "com.atproto.server.createAccount", {
      handle,
      password,
      email,
    });
    if (res.status < 500 || Date.now() > deadline) break;
    await new Promise((r) => setTimeout(r, 1_000));
  }
  if (!res.ok)
    throw new Error(
      `createAccount ${handle} ${res.status}: ${await res.text()}`,
    );
  return ((await res.json()) as { did: string }).did;
}

function xrpc(base: string, nsid: string, body: unknown) {
  return fetch(`${base}/xrpc/${nsid}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}
