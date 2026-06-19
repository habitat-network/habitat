import { useState } from "react";
import type { AuthManager } from "../authManager";
import AuthForm from "../AuthForm";
import { HostingSelector } from "./HostingSelector";

interface LoginFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  defaultDomain: string;
  serverError?: string;
  defaultHandle?: string;
  customDomain?: string;
}

export function LoginForm({
  authManager,
  redirectUrl,
  defaultDomain,
  serverError,
  defaultHandle,
  customDomain,
}: LoginFormProps) {
  const [domain, setDomain] = useState(customDomain ?? defaultDomain);

  return (
    <div className="flex flex-col gap-4">
      <HostingSelector
        defaultDomain={defaultDomain}
        value={domain}
        onChange={setDomain}
      />
      <AuthForm
        authManager={authManager}
        redirectUrl={redirectUrl}
        domain={domain}
        serverError={serverError}
        defaultHandle={defaultHandle}
      />
    </div>
  );
}
