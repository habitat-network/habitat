import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { FormEvent } from "react";

export const Route = createFileRoute('/add-user')({
  component() {
    const { mutate: handleSubmit, isPending } = useMutation({
      async mutationFn(e: FormEvent<HTMLFormElement>) {
        e.preventDefault();
        const formData = new FormData(e.target as HTMLFormElement)
        const email = formData.get('email') as string
        const handle = formData.get('handle') as string
        const password = formData.get('password') as string

        return fetch('/habitat/api/node/users', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            email,
            handle,
            password,
          }),
        })
      },
      onSuccess() {
        alert('Registration successful')
      }
    })
    return <article>
      <h1>Create user</h1>
      <form onSubmit={handleSubmit}>
        <input name="email" type="text" placeholder="Email" required />
        <input name="handle" type="text" placeholder="Handle" required />
        <input name="password" type="password" placeholder="Password" required />
        <input name="confirmPassword" type="password" placeholder="Confirm Password" required />
        <button aria-busy={isPending} type="submit">Register</button>
      </form>
    </article>
  }
});
