import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  groupQueryOptions,
  groupsListQueryOptions,
  addMember,
} from "@/queries/groups";
import { skeyOf } from "@/queries/groupUtil";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  Field,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

interface InheritableGroup {
  uri: string;
  name: string;
}

export const Route = createFileRoute("/_requireAuth/groups/$group")({
  async loader({ context, params }) {
    const { authManager, queryClient } = context;
    // Group URIs are owned by the home server's managed org, not the
    // viewer's own org (network.habitat.org.getMetadata is scoped to the
    // caller, and most callers here belong to no org at all). listGroups
    // already returns each group's full URI, so look it up there instead of
    // reconstructing it.
    const allGroups = await queryClient.ensureQueryData(
      groupsListQueryOptions(authManager),
    );
    const uri = allGroups.find((g) => skeyOf(g.uri) === params.group)?.uri;
    if (!uri) {
      throw new Error(`group not found: ${params.group}`);
    }
    const group = await queryClient.ensureQueryData(
      groupQueryOptions(uri, authManager),
    );
    return { uri, group, allGroups };
  },
  component: GroupDetail,
});

function GroupDetail() {
  const { uri, group, allGroups } = Route.useLoaderData();

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
                  <TableCell>{m.did}</TableCell>
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
        <AddMemberControls uri={uri} group={group} allGroups={allGroups} />
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
  allGroups,
}: {
  uri: string;
  group: { uri: string; inheritedGroups?: { uri: string }[] };
  allGroups: { uri: string; name: string }[];
}) {
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [selectedGroup, setSelectedGroup] = useState("");

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["group", uri] });
    await queryClient.invalidateQueries({ queryKey: ["groups"] });
    await router.invalidate();
  };

  const addUser = useMutation({
    mutationFn: () =>
      addMember(authManager, uri, { subjectDid: identifier.trim() }),
    async onSuccess() {
      await invalidate();
      setIdentifier("");
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
        <Field>
          <FieldLabel htmlFor="add-member-identifier">
            Add a person
          </FieldLabel>
          <div className="flex gap-2">
            <Input
              id="add-member-identifier"
              className="flex-1"
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              placeholder="handle.example.com or did:plc:..."
            />
            <Button
              disabled={!identifier.trim() || addUser.isPending}
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
        </Field>

        <Field>
          <FieldLabel htmlFor="inherit-group-combobox">
            Inherit members from another group
          </FieldLabel>
          <div className="flex gap-2">
            <Combobox
              items={inheritable}
              value={inheritable.find((g) => g.uri === selectedGroup) ?? null}
              onValueChange={(item) => setSelectedGroup(item?.uri ?? "")}
              itemToStringLabel={(item: InheritableGroup) => item.name}
            >
              <ComboboxInput
                id="inherit-group-combobox"
                className="flex-1"
                placeholder="Select a group…"
              />
              <ComboboxContent>
                <ComboboxEmpty>No groups to inherit from.</ComboboxEmpty>
                <ComboboxList>
                  {(item: InheritableGroup) => (
                    <ComboboxItem key={item.uri} value={item}>
                      {item.name}
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
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
        </Field>
      </CardContent>
    </Card>
  );
}
