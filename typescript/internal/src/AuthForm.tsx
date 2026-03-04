import type { AuthManager } from "./authManager";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import { useId } from "react";
import { Button, Input } from "./components/ui";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldLegend,
} from "./components/ui/field";

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
    <article className="p-4 container flex">
      <form className="w-full" onSubmit={handleSubmit((data) => login(data))}>
        <FieldGroup>
          <FieldLegend>Login</FieldLegend>
          <Field>
            <FieldLabel>Handle</FieldLabel>
            <Input
              {...register("handle")}
              aria-invalid={isError || undefined}
              aria-describedby={errorId}
            />
          </Field>
          <small id={errorId}>{error?.message}</small>
          <Button aria-busy={isPending} type="submit">
            Login
          </Button>
        </FieldGroup>
      </form>
    </article>
  );
}
