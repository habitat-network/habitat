import { useMutation } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { FormEvent } from "react";

export const Route = createFileRoute("/oauth-login")({
  validateSearch(search) {
    if (!search.code) {
      return {};
    }
    return {
      code: search.code as string,
    };
  },
  async beforeLoad({ search, context }) {
    if (search.code) {
      await context.authManager.exchangeCode(window.location.href);
      return redirect({ to: "/" });
    }
  },
  component() {
    const { authManager } = Route.useRouteContext();
    const { mutate: handleSubmit, isPending } = useMutation({
      async mutationFn(e: FormEvent<HTMLFormElement>) {
        e.preventDefault();
        const formData = new FormData(e.target as HTMLFormElement);
        const handle = formData.get("handle") as string;
        const url = authManager.loginUrl(
          handle,
          `https://${__DOMAIN__}/oauth-login`,
        );
        window.location.href = url.toString();
      },
      onError(e) {
        console.error(e);
      },
    });
    return (
      <article>
        <h1>Login</h1>
        <form onSubmit={handleSubmit}>
          <input
            name="handle"
            type="text"
            placeholder="Handle"
            required
            defaultValue={"sashankg.bsky.social"}
          />
          <button aria-busy={isPending} type="submit">
            Login
          </button>
        </form>
      </article>
    );
  },
});
