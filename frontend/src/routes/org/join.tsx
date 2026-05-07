import { Button, Field, FieldError, FieldLabel, Input } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { useState } from "react";

export const Route = createFileRoute("/org/join")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: String(search.token ?? ""),
  }),
  component: JoinPage,
});

type FormValues = { handle: string; password: string };

function JoinPage() {
  const { token } = Route.useSearch();
  const [result, setResult] = useState<{ handle: string; did: string } | null>(
    null,
  );

  const {
    register,
    handleSubmit,
    setError,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>();

  const onSubmit = async ({ handle, password }: FormValues) => {
    try {
      const res = await fetch(`https://${__HABITAT_DOMAIN__}/xrpc/network.habitat.org.mintMemberIdentity`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, handle, password }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(
          (data as { message?: string }).message ?? "Failed to join organization",
        );
      }
      setResult(await res.json());
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  if (result) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <h1 className="text-2xl font-semibold">Welcome!</h1>
        <p className="text-muted-foreground">Your account has been created.</p>
        <div className="flex flex-col gap-1 text-sm font-mono">
          <span>{result.handle}</span>
          <span className="text-muted-foreground">{result.did}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">Join Organization</h1>
      <p className="text-muted-foreground text-sm">
        Choose a handle and password for your new account.
      </p>
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
        <Field>
          <FieldLabel>Handle</FieldLabel>
          <Input
            placeholder="handle"
            disabled={isSubmitting}
            {...register("handle", { required: true })}
          />
          <FieldError errors={[errors.handle]} />
        </Field>
        <Field>
          <FieldLabel>Password</FieldLabel>
          <Input
            type="password"
            placeholder="password"
            disabled={isSubmitting}
            {...register("password", { required: true })}
          />
          <FieldError errors={[errors.password]} />
        </Field>
        <FieldError errors={[errors.root]} />
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Joining..." : "Join"}
        </Button>
      </form>
    </div>
  );
}
