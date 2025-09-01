import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { useAuth } from "@/components/authContext";

export const Route = createFileRoute('/login')({
  component() {
    const [isPending, setPending] = useState(false);
    const { isAuthenticated } = useAuth();
    const navigate = useNavigate();

    useEffect(() => {
      console.log('isAuthenticated', isAuthenticated);
      if (isAuthenticated) {
        console.log('redirecting to /');
        navigate({ to: '/' });
      }
    }, [isAuthenticated, navigate]);

    // Don't render the form if user is authenticated (will redirect)
    if (isAuthenticated) {
      return null;
    }

    return <article>
      <h1>Login</h1>
      <form method="get" action="/habitat/api/login" onSubmit={() => setPending(true)}>
        <input name="handle" type="text" placeholder="Handle" required />
        <button aria-busy={isPending} type="submit">Login</button>
      </form>
    </article>
  }
})
