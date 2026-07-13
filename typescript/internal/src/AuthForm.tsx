import type { AuthManager } from "./authManager";
import { useId } from "react";
import { Button } from "./components/ui";

interface AuthFormProps {
  authManager: AuthManager;
  serverError?: string;
  orgLoginUrl?: string;
}

export default function AuthForm({
  authManager,
  serverError,
  orgLoginUrl,
}: AuthFormProps) {
  const errorId = useId();

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-xl border bg-background p-8 shadow-sm">
        <Button
          className="w-full"
          onClick={() => authManager.login()}
          aria-describedby={errorId}
        >
          Sign In with Habitat
        </Button>
        {serverError && (
          <small id={errorId} className="mt-4 block text-destructive">
            {serverError}
          </small>
        )}
        {orgLoginUrl && (
          <Button
            variant="link"
            className="mt-6"
            size="sm"
            render={<a href={orgLoginUrl} />}
          >
            Add this app to your organization
          </Button>
        )}
      </div>
    </div>
  );
}
