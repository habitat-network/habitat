import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

// OAuth disambiguation page. pear's authorization endpoint redirects here when
// an authorize request arrives without a handle. Every original OAuth parameter
// is preserved in this page's query string; on submit we re-issue the request
// to /oauth/authorize with the same parameters plus the entered handle, which
// lets pear resolve the account and route the login.
export const Route = createFileRoute("/login/disambiguate")({
  component: DisambiguatePage,
});

type FormValues = { handle: string };

function DisambiguatePage() {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>();

  const onSubmit = ({ handle }: FormValues) => {
    // Preserve every OAuth parameter this page was loaded with and add the
    // handle, then hand back to the authorization endpoint. Reading
    // window.location.search directly avoids having to enumerate the OAuth
    // parameters in a search schema.
    const params = new URLSearchParams(window.location.search);
    params.set("handle", handle.trim());
    window.location.href = `/oauth/authorize?${params.toString()}`;
  };

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      <p className="text-sm text-muted-foreground">
        Enter your handle to continue.
      </p>
      <form onSubmit={handleSubmit(onSubmit)}>
        <fieldset className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Handle</FieldLabel>
            <Input
              placeholder="handle"
              autoFocus
              {...register("handle", { required: true })}
            />
            <FieldError errors={[errors.handle]} />
          </Field>
          <Button type="submit">Continue</Button>
        </fieldset>
      </form>
    </div>
  );
}
