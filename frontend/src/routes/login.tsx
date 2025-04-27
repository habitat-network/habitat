import { useAuth } from "@/components/authContext";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { FormEvent } from "react";

export const Route = createFileRoute('/login')({
  component() {
    const { login } = useAuth()
    const { mutate: handleSubmit, isPending } = useMutation({
      mutationFn(e: FormEvent<HTMLFormElement>) {
        e.preventDefault();
        const formData = new FormData(e.target as HTMLFormElement)
        const handle = formData.get('handle') as string
        const password = formData.get('password') as string
        return login(handle, password, null, null)
      },
    })
    return <article>
      <h1>Login</h1>
      <form onSubmit={handleSubmit}>
        <input name="handle" type="text" placeholder="Handle" required />
        <input name="password" type="password" placeholder="Password" required />
        <button aria-busy={isPending} type="submit">Login</button>
      </form>
    </article>
  }
})
