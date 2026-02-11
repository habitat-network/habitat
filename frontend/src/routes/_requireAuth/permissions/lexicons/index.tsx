import { listPermissions } from "@/queries/permissions";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { useState } from "react";

export const Route = createFileRoute("/_requireAuth/permissions/lexicons/")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component() {
    const data = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const router = useRouter();
    const [expanded, setExpanded] = useState<Set<string>>(new Set());

    const toggle = (lexicon: string) => {
      setExpanded((prev) => {
        const next = new Set(prev);
        if (next.has(lexicon)) next.delete(lexicon);
        else next.add(lexicon);
        return next;
      });
    };

    return (
      <>
        <AddNsidForm authManager={authManager} router={router} />
        <table>
          <thead>
            <tr>
              <th>NSID</th>
              <th>Permissions</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {Object.keys(data as Record<string, string[]>).map((lexicon) => (
              <>
                <tr key={lexicon}>
                  <td>{lexicon}</td>
                  <td>{(data as Record<string, string[]>)[lexicon].length}</td>
                  <td>
                    <button type="button" onClick={() => toggle(lexicon)}>
                      {expanded.has(lexicon) ? "Collapse" : "Expand"}
                    </button>
                  </td>
                </tr>
                {expanded.has(lexicon) && (
                  <tr>
                    <td colSpan={3}>
                      <LexiconDetail
                        lexicon={lexicon}
                        people={(data as Record<string, string[]>)[lexicon]}
                        authManager={authManager}
                        router={router}
                      />
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </>
    );
  },
});

function AddNsidForm({
  authManager,
  router,
}: {
  authManager: any;
  router: any;
}) {
  const form = useForm<{ did: string; lexicon: string }>();
  const { mutate: add, isPending } = useMutation({
    async mutationFn(data: { did: string; lexicon: string }) {
      await authManager?.fetch(
        `/xrpc/network.habitat.addPermission`,
        "POST",
        JSON.stringify({ did: data.did, lexicon: data.lexicon }),
      );
      form.reset();
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <details>
      <summary>Add permission</summary>
      <form onSubmit={form.handleSubmit((data) => add(data))}>
        <label>
          NSID
          <input type="text" {...form.register("lexicon")} required />
        </label>
        <label>
          DID
          <input type="text" {...form.register("did")} required />
        </label>
        <button type="submit" aria-busy={isPending}>
          Add
        </button>
      </form>
    </details>
  );
}

function LexiconDetail({
  lexicon,
  people,
  authManager,
  router,
}: {
  lexicon: string;
  people: string[];
  authManager: any;
  router: any;
}) {
  const addForm = useForm<{ did: string }>();
  const { mutate: add, isPending: isAdding } = useMutation({
    async mutationFn(data: { did: string }) {
      await authManager?.fetch(
        `/xrpc/network.habitat.addPermission`,
        "POST",
        JSON.stringify({ did: data.did, lexicon }),
      );
      addForm.reset();
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  const { mutate: remove } = useMutation({
    async mutationFn(data: { did: string }) {
      await authManager?.fetch(
        `/xrpc/network.habitat.removePermission`,
        "POST",
        JSON.stringify({ did: data.did, lexicon }),
      );
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <>
      <form onSubmit={addForm.handleSubmit((data) => add(data))}>
        <fieldset role="group">
          <input
            type="text"
            placeholder="DID to add"
            {...addForm.register("did")}
          />
          <button type="submit" aria-busy={isAdding}>
            Add
          </button>
        </fieldset>
      </form>
      <table>
        <thead>
          <tr>
            <th>Person</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {people.map((person) => (
            <tr key={person}>
              <td>{person}</td>
              <td>
                <button type="button" onClick={() => remove({ did: person })}>
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
