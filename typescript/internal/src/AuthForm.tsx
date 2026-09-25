import { hasXrpcError, type AuthManager } from "./authManager";
import SignInForm, { EMAIL_DOMAIN_NOT_FOUND_MESSAGE } from "./SignInForm";

interface AuthFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
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
          // DidNotFound is what the habitat identity resolver returns for a
          // work email at an unmapped domain.
          if (loginHint.includes("@") && hasXrpcError(err, "DidNotFound")) {
            throw new Error(EMAIL_DOMAIN_NOT_FOUND_MESSAGE, { cause: err });
          }
          throw err;
        }
      }}
    />
  );
}
