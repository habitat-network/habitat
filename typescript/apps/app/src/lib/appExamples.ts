// The example a new app can start from: one module, so someone trying Apps
// out has something to deploy before they've written any code.
export const EXAMPLE_MODULES: Record<string, string> = {
  "index.js": `// An app is a Worker: its default export's fetch handler answers every
// request to the app's URL. env.HABITAT describes the app.
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/api/hello") {
      return Response.json({ hello: url.searchParams.get("name") ?? "world" });
    }
    const { name, version } = env.HABITAT;
    return new Response(
      \`<!doctype html><meta charset="utf-8"><title>\${name}</title>
<body style="font-family:system-ui;max-width:36rem;margin:4rem auto">
<h1>\${name}</h1><p>Version \${version}, served from \${url.pathname}</p>
<p><a href="/api/hello?name=habitat">Try the API</a></p></body>\`,
      { headers: { "content-type": "text/html; charset=utf-8" } },
    );
  },
};
`,
};
