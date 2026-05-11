// @ts-check
import { defineConfig } from 'astro/config';

import tailwindcss from '@tailwindcss/vite';

import react from '@astrojs/react';

// https://astro.build/config
export default defineConfig({
  site: "https://habitat.network",
  vite: {
    plugins: [tailwindcss()],
    server: {
      allowedHosts: ['website.taile529e.ts.net']
    }
  },

  integrations: [react()]
});