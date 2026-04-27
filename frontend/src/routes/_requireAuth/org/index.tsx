import {
  getAdminsQueryOptions,
  getMembersQueryOptions,
  addAdmin,
  removeMembers,
  downgradeAdmin,
  issueInviteToken,
} from "@/queries/org";
import { Button, Input } from "internal";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
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
        onPromote={(did) => addAdmin(authManager, did).then(invalidate)}
        canPromote={true}
      />
      {isAdmin && <InviteSection authManager={authManager} />}
    </div>
  );
}

function InviteSection({
  authManager,
}: {
  authManager: Parameters<typeof issueInviteToken>[0];
}) {
  const [inviteUrl, setInviteUrl] = useState<string | null>(null);

  const { mutate: generateLink, isPending } = useMutation({
    mutationFn: () => issueInviteToken(authManager),
    onSuccess: ({ token }) => {
      setInviteUrl(`${window.location.origin}/org/join?token=${token}`);
    },
  });

  const copy = () => {
    if (!inviteUrl) return;
    navigator.clipboard.writeText(inviteUrl).then(() => {
      toast("Copied to clipboard");
    });
  };

  return (
    <section>
      <h2 className="text-lg font-semibold mb-2">Invite</h2>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={isPending}
          onClick={() => generateLink()}
        >
          Generate invite link
        </Button>
      </div>
      {inviteUrl && (
        <form
          className="flex gap-2 mt-3"
          onSubmit={(e) => {
            e.preventDefault();
            copy();
          }}
        >
          <Input
            className="flex-1 font-mono text-xs"
            readOnly
            value={inviteUrl}
            onFocus={(e) => e.currentTarget.select()}
          />
          <Button type="submit" variant="outline" size="sm">
            Copy
          </Button>
        </form>
      )}
    </section>
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
  onAdd?: (did: string) => Promise<void>;
  addLabel?: string;
  onPromote?: (did: string) => Promise<void>;
  canPromote: boolean;
}) {
  const [input, setInput] = useState("");

  const { mutate: handleAdd, isPending: adding } = useMutation({
    mutationFn: () => onAdd!(input),
    onSuccess: () => setInput(""),
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
                    <Button
                      variant="ghost"
                      size="xs"
                      onClick={() => onPromote(did)}
                    >
                      Make admin
                    </Button>
                  )}
                  <Button
                    variant="destructive"
                    size="xs"
                    onClick={() => onRemove(did)}
                  >
                    Remove
                  </Button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {isAdmin && onAdd && addLabel && (
        <div className="flex gap-2 mt-3">
          <Input
            className="flex-1 font-mono"
            placeholder="did:plc:..."
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
          <Button
            variant="outline"
            size="sm"
            disabled={!input || adding}
            onClick={() => handleAdd()}
          >
            {addLabel}
          </Button>
        </div>
      )}
    </section>
  );
}
