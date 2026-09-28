import type { AuthManager } from "./authManager";
import SignInForm, { EMAIL_DOMAIN_NOT_FOUND_MESSAGE } from "./SignInForm";

interface AuthFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
}

// isDidNotFound reports whether err, or anything in its cause chain, carries
// the XRPC error DidNotFound — what the habitat identity resolver
// (HabitatIdentityResolverError) returns for a work email at an unmapped
// domain. BrowserOAuthClient wraps resolver failures, so the resolver's error
// is usually a cause rather than err itself.
function isDidNotFound(err: unknown): boolean {
  for (let e = err; e instanceof Error; e = e.cause) {
    if ((e as { xrpcError?: unknown }).xrpcError === "DidNotFound") {
      return true;
    }
  }
  return false;
}

// AuthForm is SignInForm signing in through the browser OAuth client.
export default function AuthForm({
  authManager,
  redirectUrl,
  ...props
}: AuthFormProps) {
  return (
    <SignInForm
      {...props}
      onSubmit={async (loginHint) => {
        try {
          await authManager.login(loginHint, redirectUrl);
        } catch (err) {
          if (loginHint.includes("@") && isDidNotFound(err)) {
            throw new Error(EMAIL_DOMAIN_NOT_FOUND_MESSAGE, { cause: err });
          }
          throw err;
        }
      }}
    />
  );
}
