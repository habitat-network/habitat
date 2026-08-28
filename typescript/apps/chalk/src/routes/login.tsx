import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import {
  Button,
  Card,
  CardContent,
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  Input,
} from "internal/components/ui";
import { env } from "cloudflare:workers";
import { startLogin } from "@/server/sapClient";

// startLogin itself isn't a TanStack server function (it's a plain async
// function in sapClient.ts, shared with functions.ts's composition root) —
// wrap it here at the route boundary so the client only ever gets an RPC
// stub, never sap's internal URL or the fetch call itself.
const startLoginFn = createServerFn({ method: "POST" })
  .validator((input: { handle: string }) => input)
  .handler(async ({ data }) => ({
    redirectUrl: await startLogin(env, data.handle),
  }));

interface LoginFormData {
  handle: string;
}

export const Route = createFileRoute("/login")({
  component() {
    const {
      register,
      handleSubmit,
      formState: { errors },
    } = useForm<LoginFormData>();
    const {
      mutate: login,
      isPending,
      error,
    } = useMutation({
      async mutationFn({ handle }: LoginFormData) {
        const { redirectUrl } = await startLoginFn({
          data: { handle: handle.trim() },
        });
        window.location.href = redirectUrl;
        // Keep the button in its loading state while the browser navigates.
        await new Promise(() => {});
      },
    });

    return (
      <div className="flex items-center justify-center py-32">
        <Card className="w-full max-w-sm">
          <CardContent>
            <form onSubmit={handleSubmit((data) => login(data))}>
              <FieldSet>
                <FieldLegend>Sign In</FieldLegend>
                <FieldGroup>
                  <Field>
                    <FieldLabel>Handle</FieldLabel>
                    <Input
                      {...register("handle", {
                        required: "Handle is required",
                      })}
                      placeholder="alice.bsky.social"
                    />
                    <FieldError
                      errors={[error, errors.handle].filter((e) => !!e)}
                    />
                  </Field>
                  <Field>
                    <Button
                      loading={isPending}
                      type="submit"
                      className="w-full"
                    >
                      Sign In
                    </Button>
                  </Field>
                </FieldGroup>
              </FieldSet>
            </form>
          </CardContent>
        </Card>
      </div>
    );
  },
});
