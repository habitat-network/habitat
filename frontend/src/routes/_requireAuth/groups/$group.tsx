import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getConfigQueryOptions, getMembersQueryOptions } from "@/queries/org";
import {
  groupQueryOptions,
  groupsListQueryOptions,
  addMember,
} from "@/queries/groups";
import { groupUri, skeyOf, displayDid } from "@/queries/groupUtil";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/groups/$group")({
  async loader({ context, params }) {
    const { authManager, queryClient } = context;
    const meta = await queryClient.ensureQueryData(
      getConfigQueryOptions(authManager),
    );
    const uri = groupUri(meta.orgId, params.group);
    const [group, orgMembers, allGroups] = await Promise.all([
      queryClient.ensureQueryData(groupQueryOptions(uri, authManager)),
      queryClient.ensureQueryData(getMembersQueryOptions(authManager)),
      queryClient.ensureQueryData(groupsListQueryOptions(authManager)),
    ]);
    return { uri, group, orgMembers, allGroups };
  },
  pendingComponent: () => <p className="py-8">Loading group…</p>,
  component: GroupDetail,
});

function GroupDetail() {
  const { uri, group, orgMembers, allGroups } = Route.useLoaderData();

  const handles = new Map(orgMembers.members.map((m) => [m.did, m.handle]));
  const groupNames = new Map(allGroups.map((g) => [g.uri, g.name]));

  return (
    <div className="flex flex-col gap-6 py-6">
      <div>
        <Link
          to="/groups"
          className="text-sm text-muted-foreground hover:text-foreground"
        >
          ← All groups
        </Link>
        <div className="flex items-center gap-3 mt-2">
          <h1 className="text-2xl font-semibold">{group.name}</h1>
          {group.canManage && <Badge variant="secondary">Manager</Badge>}
          {group.isMember && <Badge variant="ghost">Member</Badge>}
        </div>
        {group.description && (
          <p className="text-muted-foreground mt-1">{group.description}</p>
        )}
      </div>

      <InheritedGroups inherited={group.inheritedGroups ?? []} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Members ({group.memberCount ?? 0})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Member</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Source</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(group.members ?? []).map((m) => (
                <TableRow key={m.did}>
                  <TableCell>{displayDid(m.did, handles)}</TableCell>
                  <TableCell className="capitalize">{m.role}</TableCell>
                  <TableCell>
                    {m.direct ? (
                      <Badge variant="outline">Direct</Badge>
                    ) : (
                      <Badge variant="ghost">
                        via{" "}
                        {groupNames.get(m.viaGroup ?? "") ?? "another group"}
                      </Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {(group.members ?? []).length === 0 && (
                <TableRow>
                  <TableCell colSpan={3} className="text-muted-foreground">
                    No members yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {group.canManage && (
        <AddMemberControls
          uri={uri}
          group={group}
          orgMembers={orgMembers.members}
          allGroups={allGroups}
        />
      )}
    </div>
  );
}

function InheritedGroups({
  inherited,
}: {
  inherited: { uri: string; name: string }[];
}) {
  if (inherited.length === 0) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Inherits members from</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-wrap gap-2">
        {inherited.map((g) => (
          <Button
            key={g.uri}
            variant="outline"
            size="sm"
            render={
              <Link to="/groups/$group" params={{ group: skeyOf(g.uri) }} />
            }
          >
            {g.name}
          </Button>
        ))}
      </CardContent>
    </Card>
  );
}

function AddMemberControls({
  uri,
  group,
  orgMembers,
  allGroups,
}: {
  uri: string;
  group: { uri: string; inheritedGroups?: { uri: string }[] };
  orgMembers: { did: string; handle: string }[];
  allGroups: { uri: string; name: string }[];
}) {
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [selectedDid, setSelectedDid] = useState("");
  const [selectedGroup, setSelectedGroup] = useState("");

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["group", uri] });
    await queryClient.invalidateQueries({ queryKey: ["groups"] });
    await router.invalidate();
  };

  const addUser = useMutation({
    mutationFn: () => addMember(authManager, uri, { subjectDid: selectedDid }),
    async onSuccess() {
      await invalidate();
      setSelectedDid("");
    },
  });

  const inheritGroup = useMutation({
    mutationFn: () =>
      addMember(authManager, uri, { subjectGroup: selectedGroup }),
    async onSuccess() {
      await invalidate();
      setSelectedGroup("");
    },
  });

  const inheritedUris = new Set(
    (group.inheritedGroups ?? []).map((g) => g.uri),
  );
  const inheritable = allGroups.filter(
    (g) => g.uri !== uri && !inheritedUris.has(g.uri),
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Manage membership</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium">Add a person</label>
          <div className="flex gap-2">
            <select
              className="border rounded px-2 py-1 text-sm flex-1 bg-background"
              value={selectedDid}
              onChange={(e) => setSelectedDid(e.target.value)}
            >
              <option value="">Select a person…</option>
              {orgMembers.map((m) => (
                <option key={m.did} value={m.did}>
                  {m.handle}
                </option>
              ))}
            </select>
            <Button
              disabled={!selectedDid || addUser.isPending}
              onClick={() => addUser.mutate()}
            >
              {addUser.isPending ? "Adding…" : "Add"}
            </Button>
          </div>
          {addUser.error && (
            <p className="text-sm text-destructive">
              {(addUser.error as Error).message}
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium">
            Inherit members from another group
          </label>
          <div className="flex gap-2">
            <select
              className="border rounded px-2 py-1 text-sm flex-1 bg-background"
              value={selectedGroup}
              onChange={(e) => setSelectedGroup(e.target.value)}
            >
              <option value="">Select a group…</option>
              {inheritable.map((g) => (
                <option key={g.uri} value={g.uri}>
                  {g.name}
                </option>
              ))}
            </select>
            <Button
              variant="outline"
              disabled={!selectedGroup || inheritGroup.isPending}
              onClick={() => inheritGroup.mutate()}
            >
              {inheritGroup.isPending ? "Adding…" : "Inherit"}
            </Button>
          </div>
          {inheritGroup.error && (
            <p className="text-sm text-destructive">
              {(inheritGroup.error as Error).message}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
