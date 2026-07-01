import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  groupsListQueryOptions,
  createGroup,
  type GroupView,
} from "@/queries/groups";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  Label,
  Textarea,
} from "internal/components/ui";
import { skeyOf } from "@/queries/groupUtil";

export const Route = createFileRoute("/_requireAuth/groups/")({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      groupsListQueryOptions(context.authManager),
    ),
  pendingComponent: () => <p className="py-8">Loading groups…</p>,
  component: GroupsList,
});

function GroupsList() {
  const groups = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-6 py-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Groups</h1>
          <p className="text-muted-foreground text-sm">
            Groups you belong to, directly or through inherited groups.
          </p>
        </div>
        <CreateGroupDialog />
      </div>

      {groups.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            You aren’t a member of any groups yet. Create one to get started.
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {groups.map((group) => (
            <GroupCard key={group.uri} group={group} />
          ))}
        </div>
      )}
    </div>
  );
}

function GroupCard({ group }: { group: GroupView }) {
  return (
    <Card className="hover:border-primary transition-colors">
      <Link to="/groups/$group" params={{ group: skeyOf(group.uri) }}>
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-2">
            <span className="truncate">{group.name}</span>
            {group.canManage && <Badge variant="secondary">Manager</Badge>}
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {group.description && (
            <p className="text-sm text-muted-foreground line-clamp-2">
              {group.description}
            </p>
          )}
          <div className="flex items-center gap-2 flex-wrap text-xs">
            <Badge variant="outline">
              {group.memberCount ?? 0} member
              {group.memberCount === 1 ? "" : "s"}
            </Badge>
            {group.isMember && <Badge variant="ghost">You’re a member</Badge>}
          </div>
          {group.inheritedGroups && group.inheritedGroups.length > 0 && (
            <div className="text-xs text-muted-foreground">
              Inherits from{" "}
              {group.inheritedGroups.map((g) => g.name).join(", ")}
            </div>
          )}
        </CardContent>
      </Link>
    </Card>
  );
}

function CreateGroupDialog() {
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => createGroup(authManager, name, description),
    async onSuccess() {
      await queryClient.invalidateQueries({ queryKey: ["groups"] });
      await router.invalidate();
      setOpen(false);
      setName("");
      setDescription("");
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button>New group</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create a group</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) mutate();
          }}
        >
          <div className="flex flex-col gap-2">
            <Label htmlFor="group-name">Name</Label>
            <Input
              id="group-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Engineering"
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="group-description">Description</Label>
            <Textarea
              id="group-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What is this group for?"
            />
          </div>
          {error && (
            <p className="text-sm text-destructive">
              {(error as Error).message}
            </p>
          )}
          <DialogFooter>
            <Button type="submit" disabled={isPending || !name.trim()}>
              {isPending ? "Creating…" : "Create group"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
