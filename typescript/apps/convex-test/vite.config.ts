import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { defineConfig, loadEnv } from 'vite'
import tsConfigPaths from 'vite-tsconfig-paths'
import tailwindcss from '@tailwindcss/vite'
import viteReact from '@vitejs/plugin-react'

// Vite only exposes .env values via import.meta.env (VITE_-prefixed by
// default); TanStack Start's server functions read process.env directly
// (see src/server/sapClient.ts, src/server/session.ts), so load all env
// vars (empty 3rd arg = no VITE_ prefix filter) and mirror them onto
// process.env for those reads to see.
const env = loadEnv(process.env.NODE_ENV ?? 'development', process.cwd(), '')
Object.assign(process.env, env)

export default defineConfig({
  server: {
    port: 3000,
  },
  plugins: [
    tailwindcss(),
    tsConfigPaths({
      projects: ['./tsconfig.json'],
    }),
    tanstackStart(),
    viteReact(),
  ],
})
