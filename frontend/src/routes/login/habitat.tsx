import { Button, Field, FieldError, FieldLabel, Input } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

export const Route = createFileRoute("/login/habitat")({
  validateSearch: (search: Record<string, unknown>) => ({
    handle: String(search.handle ?? ""),
  }),
  component: HabitatLoginPage,
});

type FormValues = { password: string };

function HabitatLoginPage() {
  const { handle } = Route.useSearch();

  const {
    register,
    handleSubmit,
    setError,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>();

  const onSubmit = async ({ password }: FormValues) => {
    try {
      // Bypass authManager: this is the login endpoint itself, so the user has
      // no session yet. Going through authManager.fetch would short-circuit
      // and redirect to /oauth-login before the request fires.
      const res = await fetch(
        `https://${__HABITAT_DOMAIN__}/xrpc/network.habitat.org.loginMember`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ handle, password }),
        },
      );
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `Login failed (${res.status})`);
      }
      const { callbackURL } = (await res.json()) as { callbackURL: string };
      window.location.href = `https://${__HABITAT_DOMAIN__}${callbackURL}`;
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      {handle && (
        <p className="text-sm text-muted-foreground font-mono">{handle}</p>
      )}
      <form onSubmit={handleSubmit(onSubmit)}>
        <fieldset disabled={isSubmitting} className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Password</FieldLabel>
            <Input
              type="password"
              placeholder="password"
              {...register("password", { required: true })}
            />
            <FieldError errors={[errors.password]} />
          </Field>
          <FieldError errors={[errors.root]} />
          <Button type="submit">
            {isSubmitting ? "Signing in..." : "Sign in"}
          </Button>
        </fieldset>
      </form>
    </div>
  );
}
