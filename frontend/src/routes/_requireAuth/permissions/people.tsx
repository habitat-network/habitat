import { listPermissions } from "@/queries/permissions";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { Permission } from "api/types/network/habitat/permissions/listPermissions";
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

export const Route = createFileRoute("/_requireAuth/permissions/people")({
  async loader({ context }) {
    return context.queryClient.fetchQuery(listPermissions(context.authManager));
  },
  component: PeoplePermissions,
});

function PeoplePermissions() {
  const data = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (person: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(person)) next.delete(person);
      else next.add(person);
      return next;
    });
  };

  const byPerson = (data.permissions ?? []).reduce<
    Record<string, Permission[]>
  >((acc, perm) => {
    (acc[perm.grantee] ??= []).push(perm);
    return acc;
  }, {});

  return (
    <table>
      <thead>
        <tr>
          <th>Person</th>
          <th>Permissions</th>
          <th />
        </tr>
      </thead>
      {Object.entries(byPerson).map(([person, perms]) => (
        <tbody key={person}>
          <tr>
            <td>{person}</td>
            <td>{perms.length}</td>
            <td>
              <button type="button" onClick={() => toggle(person)}>
                {expanded.has(person) ? "Collapse" : "Expand"}
              </button>
            </td>
          </tr>
          {expanded.has(person) && (
            <tr>
              <td colSpan={3}>
                <PersonDetail
                  person={person}
                  permissions={perms}
                  authManager={authManager}
                />
              </td>
            </tr>
          )}
        </tbody>
      ))}
    </table>
  );
}

function PersonDetail({
  person,
  permissions,
  authManager,
}: {
  person: string;
  permissions: Permission[];
  authManager: any;
}) {
  const queryClient = useQueryClient();
  const router = useRouter();

  const { mutate: remove } = useMutation({
    async mutationFn({
      collection,
      rkey,
    }: {
      collection: string;
      rkey: string | undefined;
    }) {
      const body: PermissionInput = {
        grantees: [
          { $type: "network.habitat.grantee#didGrantee", did: person },
        ],
        collection,
        ...(rkey ? { rkey } : {}),
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
    <table>
      <thead>
        <tr>
          <th>Collection</th>
          <th>Record Key</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {permissions.map((perm) => (
          <tr key={`${perm.collection}:${perm.rkey}`}>
            <td>{perm.collection}</td>
            <td>{perm.rkey || "*"}</td>
            <td>
              <button
                type="button"
                onClick={() =>
                  remove({ collection: perm.collection, rkey: perm.rkey })
                }
              >
                Remove
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
