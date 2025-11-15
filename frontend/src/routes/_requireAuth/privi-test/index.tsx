import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/privi-test/")({
  component() {
    const { authManager } = Route.useRouteContext();
    const { mutate } = useMutation({
      async mutationFn() {
        const response = await authManager.fetch(
          "https://privi.dwelf-mirzam.ts.net/xrpc/com.habitat.putRecord",
          "POST",
          JSON.stringify({
            collection: "com.habitat.test",
            record: {
              foo: "bar",
            },
            repo: authManager.handle,
            rkey: "testRecord",
          }),
        );
        console.log(response);
      },
    });
    return (
      <article>
        <h1>Privi Test</h1>
        <button onClick={() => mutate()}>Test</button>
      </article>
    );
  },
});
