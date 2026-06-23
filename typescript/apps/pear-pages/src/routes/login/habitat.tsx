import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

// Member password login page. pear redirects here (from the password login
// provider's Authorize step) with the member's handle as a search param. The
// page is served same-origin by pear under /ui/, so it calls the loginMember
// XRPC endpoint directly and follows the returned OAuth callback URL.
export const Route = createFileRoute("/login/habitat")({
  validateSearch: (search: Record<string, unknown>) => ({
    handle: String(search.handle ?? ""),
  }),
  component: HabitatLoginPage,
});

type FormValues = { password: string };

type LoginMemberOutput = { callbackURL: string };

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
      const res = await fetch("/xrpc/network.habitat.org.loginMember", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ handle, password }),
      });
      if (!res.ok) {
        throw new Error((await res.text()) || "Login failed");
      }
      const { callbackURL } = (await res.json()) as LoginMemberOutput;
      window.location.href = callbackURL;
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      {handle && (
        <p className="font-mono text-sm text-muted-foreground">{handle}</p>
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
