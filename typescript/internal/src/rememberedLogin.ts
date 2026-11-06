// Reads and clears the login-hint cookie pear sets after a user signs in
// (internal/oauthserver/login_hint_cookie.go). It is scoped to the shared
// parent domain when pear runs with --login_hint_cookie_domain, so every app
// under that domain can see who last signed in and skip asking for a handle.
//
// The cookie is only a hint of who to sign in as, never proof of sign-in:
// passing its DID to the OAuth client still sends the user through their
// real login.

export const LOGIN_HINT_COOKIE_NAME = "habitat_login_hint";

export interface RememberedLogin {
  did: string;
  // Empty when pear couldn't resolve a valid handle for the DID.
  handle?: string;
}

// readRememberedLogin returns who last signed in through pear, if this page
// can see the cookie. The cookie value is base64url(JSON) + "." + signature;
// only pear checks the signature, so treat the result as untrusted input.
export function readRememberedLogin(
  cookies: string = typeof document === "undefined" ? "" : document.cookie,
): RememberedLogin | undefined {
  for (const part of cookies.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name !== LOGIN_HINT_COOKIE_NAME) {
      continue;
    }
    const [payload] = rest.join("=").split(".");
    try {
      const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
      const parsed: unknown = JSON.parse(json);
      if (
        typeof parsed === "object" &&
        parsed !== null &&
        "did" in parsed &&
        typeof parsed.did === "string" &&
        parsed.did.startsWith("did:")
      ) {
        const handle =
          "handle" in parsed && typeof parsed.handle === "string"
            ? parsed.handle
            : undefined;
        return { did: parsed.did, handle: handle || undefined };
      }
    } catch {
      // A malformed cookie is the same as none.
    }
    return undefined;
  }
  return undefined;
}

// forgetRememberedLogin clears the cookie so the next sign-in asks for a
// handle again. A cookie can only be deleted with the Domain it was set with,
// which this page doesn't know, so expire it as a host-only cookie and on
// every parent domain of this host; the browser ignores the ones that don't
// apply.
export function forgetRememberedLogin(hostname: string = location.hostname) {
  const expire = `${LOGIN_HINT_COOKIE_NAME}=; Path=/; Max-Age=0; Secure; SameSite=Lax`;
  document.cookie = expire;
  const labels = hostname.split(".");
  for (let i = 0; i < labels.length - 1; i++) {
    document.cookie = `${expire}; Domain=${labels.slice(i).join(".")}`;
  }
}
