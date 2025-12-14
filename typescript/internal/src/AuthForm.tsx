import type { AuthManager } from "./authManager";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import { useId } from "react";

interface AuthFormData {
  handle?: string;
}

interface AuthFormProps {
  authManager: AuthManager;
  redirectUrl: string;
}

export default function AuthForm({ authManager, redirectUrl }: AuthFormProps) {
  const { register, handleSubmit } = useForm<AuthFormData>();
  const {
    mutate: login,
    isPending,
    error,
    isError,
  } = useMutation({
    async mutationFn({ handle }: AuthFormData) {
      if (!handle) {
        throw new Error("Handle required");
      }
      const url = authManager.loginUrl(handle, redirectUrl);
      window.location.href = url.toString();
    },
  });
  const errorId = useId();

  return (
    <article>
      <h1>Login</h1>
      <form onSubmit={handleSubmit((data) => login(data))}>
        <input
          {...register("handle")}
          aria-invalid={isError || undefined}
          aria-describedby={errorId}
        />
        <small id={errorId}>{error?.message}</small>
        <button aria-busy={isPending} type="submit">
          Login
        </button>
      </form>
    </article>
  );
}
