import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

export const Route = createFileRoute('/_requireAuth')({
  beforeLoad({ context }) {
    if (!context.authSession) {
      throw redirect({ to: '/oauth-login' })
    }
  },
  component() {
    return <Outlet />
  }
})
