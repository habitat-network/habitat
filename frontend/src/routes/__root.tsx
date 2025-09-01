import { AuthProvider } from '@/components/authContext'
import Header from '@/components/header'
import type { BrowserOAuthClient, OAuthSession } from '@atproto/oauth-client-browser'
import { type QueryClient } from '@tanstack/react-query'
import { Outlet, createRootRouteWithContext } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'

interface RouterContext {
  queryClient: QueryClient
  oauthClient: BrowserOAuthClient
  authSession?: OAuthSession
}

export const Route = createRootRouteWithContext<RouterContext>()({
  async beforeLoad({ context }) {
    const result = await context.oauthClient.init()
    return {
      authSession: result?.session,
    }
  },
  staleTime: 1000 * 60 * 60,
  async loader({ context }) {
    if (!context.authSession) {
      return {}
    }
    const response = await context.authSession.fetchHandler(`/xrpc/com.atproto.repo.describeRepo?repo=${context.authSession.did}`)
    const details = await response.json() as { handle: string }
    return {
      handle: details.handle,
    }
  },
  component() {
    return (
      <AuthProvider>
        <Header />
        <Outlet />
        <TanStackRouterDevtools />
      </AuthProvider>
    )
  },
})
