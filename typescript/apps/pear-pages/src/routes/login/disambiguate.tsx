import {
  Button,
  Field,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";

// OAuth disambiguation page. pear's authorization endpoint redirects here when
// an authorize request arrives without a handle. Every original OAuth parameter
// is preserved in this page's query string; on submit we re-issue the request
// to /oauth/authorize with the same parameters plus the entered handle, which
// lets pear resolve the account and route the login.
export const Route = createFileRoute("/login/disambiguate")({
  component: DisambiguatePage,
});

function DisambiguatePage() {
  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      <p className="text-sm text-muted-foreground">
        Enter your handle to continue.
      </p>
      <form action="/oauth/authorize">
        <fieldset className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Handle</FieldLabel>
            <Input
              placeholder="handle"
              autoFocus
              name="handle"
              required
            />
          </Field>
          <Button type="submit">Continue</Button>
        </fieldset>
      </form>
    </div>
  );
}
