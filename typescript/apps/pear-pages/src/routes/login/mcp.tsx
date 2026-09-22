import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { useForm } from "react-hook-form";

// MCP OAuth handle prompt. pear's MCP authorization endpoint
// (internal/mcpoauth) redirects here once it has validated the client's
// request, carrying that request's id and the requesting client's name as
// search params. Submitting posts the handle back to
// /mcp/oauth/authorize/submit, which starts the standard atproto OAuth flow
// against that account's PDS and returns the URL to send the browser to next.
export const Route = createFileRoute("/login/mcp")({
  validateSearch: z.object({
    request_id: z.string().default(""),
    client_name: z.string().default(""),
    login_hint: z.string().default(""),
  }),
  component: McpLoginPage,
});

type FormValues = { handle: string };

function McpLoginPage() {
  const { request_id: requestId, client_name: clientName, login_hint: loginHint } =
    Route.useSearch();

  const {
    register,
    handleSubmit,
    setError,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>({ defaultValues: { handle: loginHint } });

  const onSubmit = async ({ handle }: FormValues) => {
    try {
      const res = await fetch("/mcp/oauth/authorize/submit", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ requestId, handle }),
      });
      const body = (await res.json()) as {
        redirect?: string;
        message?: string;
      };
      if (!res.ok) {
        throw new Error(body.message || "Sign in failed");
      }
      window.location.href = body.redirect ?? "";
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Sign in to Habitat</h1>
      <p className="text-sm text-muted-foreground">
        {clientName ? (
          <>
            <span className="font-medium text-foreground">{clientName}</span>{" "}
            wants to connect to your Habitat data.
          </>
        ) : (
          "An application wants to connect to your Habitat data."
        )}{" "}
        Enter your handle to sign in.
      </p>
      <form onSubmit={handleSubmit(onSubmit)}>
        <fieldset disabled={isSubmitting} className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Handle</FieldLabel>
            <Input
              placeholder="alice.example.com"
              autoFocus
              autoCapitalize="none"
              {...register("handle", { required: true })}
            />
            <FieldError errors={[errors.handle]} />
          </Field>
          <FieldError errors={[errors.root]} />
          <Button type="submit">
            {isSubmitting ? "Signing in..." : "Continue"}
          </Button>
        </fieldset>
      </form>
    </div>
  );
}
