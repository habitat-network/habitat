import { listPermissions } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { useState } from "react";

// Concrete wire types matching what the server's parseGrantees expects
interface DidGranteeObj {
  $type: "network.habitat.grantee#didGrantee";
  did: string;
}

interface PermissionInput {
  grantees: DidGranteeObj[];
  collection: string;
  rkey?: string;
}

export const Route = createFileRoute("/_requireAuth/permissions/lexicons/")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component: LexiconPermissions,
});

function LexiconPermissions() {
  const data = Route.useLoaderData() as Record<string, string[]>;
  const { authManager } = Route.useRouteContext();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (lexicon: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(lexicon)) next.delete(lexicon);
      else next.add(lexicon);
      return next;
    });
  };

  const addForm = useForm<{ grantee: string; collection: string; rkey: string }>(
    { defaultValues: { rkey: "" } },
  );
  const { mutate: addNew, isPending: isAddingNew } = useMutation({
    async mutationFn(formData: { grantee: string; collection: string; rkey: string }) {
      const body: PermissionInput = {
        grantees: [{ $type: "network.habitat.grantee#didGrantee", did: formData.grantee }],
        collection: formData.collection,
        ...(formData.rkey ? { rkey: formData.rkey } : {}),
      };
      await authManager?.fetch(
        `/xrpc/network.habitat.addPermission`,
        "POST",
        JSON.stringify(body),
        new Headers({ "Content-Type": "application/json" }),
      );
      addForm.reset({ rkey: "" });
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <>
      <h3>Add permission</h3>
      <form onSubmit={addForm.handleSubmit((d) => addNew(d))}>
        <label>
          Collection
          <input type="text" {...addForm.register("collection")} required />
        </label>
        <label>
          Record key (optional)
          <input type="text" {...addForm.register("rkey")} />
        </label>
        <label>
          DID
          <input type="text" {...addForm.register("grantee")} required />
        </label>
        <button type="submit" aria-busy={isAddingNew}>
          Add
        </button>
      </form>
      <table>
        <thead>
          <tr>
            <th>NSID</th>
            <th>Permissions</th>
            <th />
          </tr>
        </thead>
        {Object.keys(data).map((lexicon) => (
          <tbody key={lexicon}>
            <tr>
              <td>{lexicon}</td>
              <td>{data[lexicon].length}</td>
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
                    people={data[lexicon]}
                    authManager={authManager}
                    router={router}
                  />
                </td>
              </tr>
            )}
          </tbody>
        ))}
      </table>
    </>
  );
}

function LexiconDetail({
  lexicon,
  people,
  authManager,
}: {
  lexicon: string;
  people: string[];
  authManager: any;
  router: any;
}) {
  const queryClient = useQueryClient();
  const router = useRouter();
  const addForm = useForm<{ grantee: string }>();
  const { mutate: add, isPending: isAdding } = useMutation({
    async mutationFn(data: { grantee: string }) {
      const body: PermissionInput = {
        grantees: [{ $type: "network.habitat.grantee#didGrantee", did: data.grantee }],
        collection: lexicon,
      };
      await authManager?.fetch(
        `/xrpc/network.habitat.addPermission`,
        "POST",
        JSON.stringify(body),
        new Headers({ "Content-Type": "application/json" }),
      );
      addForm.reset();
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  const { mutate: remove } = useMutation({
    async mutationFn(grantee: string) {
      const body: PermissionInput = {
        grantees: [{ $type: "network.habitat.grantee#didGrantee", did: grantee }],
        collection: lexicon,
      };
      await authManager?.fetch(
        `/xrpc/network.habitat.removePermission`,
        "POST",
        JSON.stringify(body),
        new Headers({ "Content-Type": "application/json" }),
      );
      await queryClient.invalidateQueries({ queryKey: ["permissions"] });
      router.invalidate();
    },
    onError(e: Error) {
      console.error(e);
    },
  });

  return (
    <>
      <form onSubmit={addForm.handleSubmit((data) => add(data))}>
        <fieldset>
          <input
            type="text"
            placeholder="DID to add"
            {...addForm.register("grantee")}
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
                <button type="button" onClick={() => remove(person)}>
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
