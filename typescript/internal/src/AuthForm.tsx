import type { AuthManager } from "./authManager";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import { useId } from "react";
import { Button, Input } from "./components/ui";
import { Field, FieldGroup, FieldLabel } from "./components/ui/field";

interface AuthFormData {
  handle?: string;
}

interface AuthFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
}

export default function AuthForm({
  authManager,
  redirectUrl,
  serverError,
  defaultHandle,
  orgLoginUrl,
}: AuthFormProps) {
  const { register, handleSubmit } = useForm<AuthFormData>();
  const {
    mutate: login,
    isPending,
    error,
    isError,
  } = useMutation({
    async mutationFn({ handle }: AuthFormData) {
      // if (!handle) {
      //   throw new Error("Handle required");
      // }
      const url = authManager.loginUrl(handle || "", redirectUrl);
      window.location.href = url.toString();
    },
  });
  const errorId = useId();

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-xl border bg-background p-8 shadow-sm">
        <form onSubmit={handleSubmit((data) => login(data))}>
          <FieldGroup className="mt-6 space-y-4">
            <Field>
              <FieldLabel>Handle</FieldLabel>
              <Input
                {...register("handle")}
                defaultValue={defaultHandle}
                aria-invalid={isError || !!serverError || undefined}
                aria-describedby={errorId}
                placeholder="alice.bsky.social"
              />
            </Field>
            {serverError && (
              <small id={errorId} className="text-destructive">
                {serverError}
              </small>
            )}
            {!serverError && error?.message && (
              <small id={errorId} className="text-destructive">
                {error.message}
              </small>
            )}
            <Button aria-busy={isPending} type="submit" className="w-full">
              Sign In
            </Button>
          </FieldGroup>
        </form>
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
