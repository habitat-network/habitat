import { setupServer } from "msw/node";

// Shared msw server. Tests add per-test handlers with server.use(...); the
// setup file resets them after each test.
export const server = setupServer();

// The habitat instance's origin, matching VITE_HABITAT_DOMAIN in
// vitest.config.ts.
export const PEAR = "https://pear.test";
