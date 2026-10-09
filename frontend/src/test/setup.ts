import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { server } from "./server";

// jsdom lacks these, which base-ui components (dropdowns, dialogs) rely on.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
Element.prototype.scrollIntoView ??= () => {};

beforeAll(() => {
  // Fail loudly on any request a test forgot to mock.
  server.listen({
    onUnhandledRequest(request, print) {
      console.error(`unmocked request: ${request.method} ${request.url}`);
      print.error();
    },
  });
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});
