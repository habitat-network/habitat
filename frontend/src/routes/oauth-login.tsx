import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { FormEvent } from "react";

export const Route = createFileRoute('/oauth-login')({
  component() {
    const { oauthClient } = Route.useRouteContext()
    const { mutate: handleSubmit, isPending } = useMutation({
      async mutationFn(e: FormEvent<HTMLFormElement>) {
        e.preventDefault();
        const formData = new FormData(e.target as HTMLFormElement)
        const handle = formData.get('handle') as string
        await oauthClient.signIn(handle)
      },
      onError(e) {
        console.error(e)
      }
    })
    return <article>
      <h1>Login</h1>
      <form onSubmit={handleSubmit}>
        <input name="handle" type="text" placeholder="Handle" required />
        <button aria-busy={isPending} type="submit">Login</button>
      </form>
    </article>
  }
})
