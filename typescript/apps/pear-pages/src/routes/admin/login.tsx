import { Button, Field, FieldLabel, Input } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";

// Instance admin login page (migrated from internal/instance/login.html).
// The form posts to pear's `/admin/login` handler, which validates the
// password, creates a session cookie and redirects. On failure pear redirects
// back here with an `error` search param.
export const Route = createFileRoute("/admin/login")({
  validateSearch: (search: Record<string, unknown>) => ({
    error: typeof search.error === "string" ? search.error : "",
  }),
  component: AdminLoginPage,
});

function AdminLoginPage() {
  const { error } = Route.useSearch();

  return (
    <main className="w-80 rounded-[0.625rem] border border-border bg-card p-8 shadow-sm">
      <h1 className="mb-1 text-xl font-semibold">Habitat</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        Sign in to manage this instance
      </p>
      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}
      <form method="POST" action="/admin/login" className="flex flex-col gap-4">
        <Field>
          <FieldLabel htmlFor="password">Password</FieldLabel>
          <Input type="password" name="password" id="password" autoFocus />
        </Field>
        <Button type="submit">Log in</Button>
      </form>
    </main>
  );
}
