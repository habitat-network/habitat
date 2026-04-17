import {
  getAdminsQueryOptions,
  getMembersQueryOptions,
  addAdmin,
  addMembers,
  removeAdmin,
  removeMembers,
  downgradeAdmin,
} from "internal";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";

export const Route = createFileRoute("/_requireAuth/org/")({
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const [adminsData, membersData] = await Promise.all([
      queryClient.fetchQuery(getAdminsQueryOptions(authManager)),
      queryClient.fetchQuery(getMembersQueryOptions(authManager)),
    ]);
    const authInfo = authManager.getAuthInfo();
    const isAdmin = adminsData.admins.includes(authInfo?.did ?? "");
    const adminSet = new Set(adminsData.admins);
    const members = membersData.members.filter((did) => !adminSet.has(did));
    return { admins: adminsData.admins, members, isAdmin };
  },
  component: OrgPage,
});

function OrgPage() {
  const { admins, members, isAdmin } = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["org"] });
    router.invalidate();
  };

  return (
    <div className="flex flex-col gap-8">
      <MemberSection
        title="Admins"
        dids={admins}
        isAdmin={isAdmin}
        onRemove={(did) => downgradeAdmin(authManager, did).then(invalidate)}
        addLabel="Add admin"
        onAdd={(did) => addAdmin(authManager, did).then(invalidate)}
        canPromote={false}
      />
      <MemberSection
        title="Members"
        dids={members}
        isAdmin={isAdmin}
        onRemove={(did) => removeMembers(authManager, [did]).then(invalidate)}
        addLabel="Add member"
        onAdd={(did) => addMembers(authManager, [did]).then(invalidate)}
        onPromote={(did) => addAdmin(authManager, did).then(invalidate)}
        canPromote={true}
      />
    </div>
  );
}

function MemberSection({
  title,
  dids,
  isAdmin,
  onRemove,
  onAdd,
  addLabel,
  onPromote,
  canPromote,
}: {
  title: string;
  dids: string[];
  isAdmin: boolean;
  onRemove: (did: string) => Promise<void>;
  onAdd: (did: string) => Promise<void>;
  addLabel: string;
  onPromote?: (did: string) => Promise<void>;
  canPromote: boolean;
}) {
  const [input, setInput] = useState("");

  const { mutate: handleAdd, isPending: adding } = useMutation({
    mutationFn: () => onAdd(input),
    onSuccess: () => setInput(""),
  });

  const { mutate: handleRemove } = useMutation({
    mutationFn: (did: string) => onRemove(did),
  });

  const { mutate: handlePromote } = useMutation({
    mutationFn: (did: string) => onPromote!(did),
  });

  return (
    <section>
      <h2 className="text-lg font-semibold mb-2">{title}</h2>
      <table className="w-full text-sm">
        <tbody>
          {dids.map((did) => (
            <tr key={did} className="border-b">
              <td className="py-2 pr-4 font-mono">{did}</td>
              {isAdmin && (
                <td className="py-2 flex gap-2 justify-end">
                  {canPromote && onPromote && (
                    <button
                      type="button"
                      className="text-xs underline"
                      onClick={() => handlePromote(did)}
                    >
                      Make admin
                    </button>
                  )}
                  <button
                    type="button"
                    className="text-xs underline text-red-600"
                    onClick={() => handleRemove(did)}
                  >
                    Remove
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {isAdmin && (
        <div className="flex gap-2 mt-3">
          <input
            className="border rounded px-2 py-1 text-sm flex-1 font-mono"
            placeholder="did:plc:..."
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
          <button
            type="button"
            className="border rounded px-3 py-1 text-sm"
            disabled={!input || adding}
            onClick={() => handleAdd()}
          >
            {addLabel}
          </button>
        </div>
      )}
    </section>
  );
}
