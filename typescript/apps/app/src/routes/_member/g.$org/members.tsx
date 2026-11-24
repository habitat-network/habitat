import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { UserAvatar } from "internal";
import {
  Badge,
  Button,
  Card,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  toast,
} from "internal/components/ui";
import { MailPlus, MoreVertical, UserMinus } from "lucide-react";
import { useMemo, useState } from "react";
import {
  EmptyState,
  PageBody,
  PageHeader,
} from "@/components/shell/PageHeader";
import { useHabitatActors } from "@/hooks/useHabitatActors";
import {
  groupInvitesQuery,
  groupLabel,
  groupQuery,
  membersQuery,
} from "@/lib/queries";
import { rolesQuery } from "@/lib/roles";
import {
  assignRoles,
  inviteMember,
  removeMember,
  revokeInvite,
  type MemberView,
  type RoleView,
} from "@/server/fns/manage";

// Members: the group's roster and its outstanding invites. pear checks
// every change (invite, eject, role.assign); the page only hides controls
// the member's roles don't grant.

export const Route = createFileRoute("/_member/g/$org/members")({
  validateSearch: z.object({ tab: z.enum(["members", "invites"]).optional() }),
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(groupQuery(params.org)),
      context.queryClient.ensureQueryData(membersQuery(params.org)),
      context.queryClient.ensureQueryData(groupInvitesQuery(params.org)),
      context.queryClient.ensureQueryData(rolesQuery(params.org)),
    ]),
  component: MembersPage,
});

const dateFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
});

function MembersPage() {
  const { org } = Route.useParams();
  const { tab = "members" } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { member } = Route.useRouteContext();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  const { data: members } = useSuspenseQuery(membersQuery(org));
  const { data: invites } = useSuspenseQuery(groupInvitesQuery(org));
  // Invitees aren't members yet, so pear has no group profile for them;
  // resolve their handles the way the doc share dialog does.
  const inviteeDids = useMemo(() => invites.map((i) => i.invitee), [invites]);
  const getInvitee = useHabitatActors(inviteeDids);
  const { data: roles } = useSuspenseQuery(rolesQuery(org));
  const can = (action: string) => group.myActions.includes(action);

  return (
    <>
      <PageHeader
        title="Members"
        description={`${members.length} in ${groupLabel(group)}`}
      >
        {can("invite") && <InviteDialog org={org} roles={roles.roles} />}
      </PageHeader>
      <PageBody>
        <Tabs
          value={tab}
          onValueChange={(v) =>
            navigate({
              search: { tab: v === "members" ? undefined : (v as "invites") },
            })
          }
        >
          <TabsList>
            <TabsTrigger value="members">
              Members · {members.length}
            </TabsTrigger>
            <TabsTrigger value="invites">
              Invites · {invites.length}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="members" className="mt-4">
            <Card className="overflow-hidden p-0">
              <Table>
                <TableHeader className="bg-muted/40">
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-full pl-5">Member</TableHead>
                    <TableHead>Roles</TableHead>
                    <TableHead className="hidden md:table-cell">
                      Since
                    </TableHead>
                    <TableHead className="w-10 pr-5">
                      <span className="sr-only">Actions</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {members.map((m) => (
                    <MemberRow
                      key={m.did}
                      org={org}
                      member={m}
                      isMe={m.did === member.did}
                      roles={roles?.roles ?? []}
                      canAssign={can("role.assign")}
                      canEject={can("eject")}
                    />
                  ))}
                </TableBody>
              </Table>
            </Card>
          </TabsContent>
          <TabsContent value="invites" className="mt-4">
            {invites.length === 0 ? (
              <EmptyState icon={<MailPlus />} title="No pending invites">
                {can("invite")
                  ? "Invite someone by their handle; they'll see it under Join a group."
                  : "Members with the invite permission can invite people here."}
              </EmptyState>
            ) : (
              <Card className="overflow-hidden p-0">
                <Table>
                  <TableHeader className="bg-muted/40">
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-full pl-5">Invitee</TableHead>
                      <TableHead>Roles</TableHead>
                      <TableHead className="hidden md:table-cell">
                        Sent
                      </TableHead>
                      <TableHead className="pr-5" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {invites.map((i) => {
                      const handle =
                        i.inviteeHandle ?? getInvitee(i.invitee).handle;
                      return (
                        <TableRow key={i.id}>
                          <TableCell className="max-w-0 pl-5">
                            <div className="flex min-w-0 items-center gap-3">
                              <UserAvatar
                                actor={{
                                  did: i.invitee,
                                  handle,
                                  displayName: i.inviteeName ?? undefined,
                                }}
                                size="sm"
                              />
                              <div className="min-w-0">
                                <p className="truncate font-medium">
                                  {i.inviteeName || handle || i.invitee}
                                </p>
                                {handle && (
                                  <p className="truncate text-xs text-muted-foreground">
                                    @{handle}
                                  </p>
                                )}
                              </div>
                            </div>
                          </TableCell>
                          <TableCell>
                            <RoleBadges
                              ids={i.roles}
                              roles={roles?.roles ?? []}
                            />
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap text-muted-foreground md:table-cell">
                            {dateFormat.format(new Date(i.createdAt))}
                          </TableCell>
                          <TableCell className="pr-5 text-right">
                            <RevokeButton org={org} id={i.id} />
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </Card>
            )}
          </TabsContent>
        </Tabs>
      </PageBody>
    </>
  );
}

function RoleBadges({ ids, roles }: { ids: string[]; roles: RoleView[] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {ids.map((id) => (
        <Badge key={id} variant={id === "admin" ? "default" : "secondary"}>
          {roles.find((r) => r.id === id)?.name ?? id}
        </Badge>
      ))}
    </div>
  );
}

function MemberRow({
  org,
  member,
  isMe,
  roles,
  canAssign,
  canEject,
}: {
  org: string;
  member: MemberView;
  isMe: boolean;
  roles: RoleView[];
  canAssign: boolean;
  canEject: boolean;
}) {
  const queryClient = useQueryClient();
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["members", org] }),
      queryClient.invalidateQueries({ queryKey: ["roles", org] }),
      queryClient.invalidateQueries({ queryKey: ["group", org] }),
    ]);
  const assign = useMutation({
    mutationFn: (next: string[]) =>
      assignRoles({ data: { org, member: member.did, roles: next } }),
    onSuccess: invalidate,
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't change roles",
        description: e.message,
      }),
  });
  const remove = useMutation({
    mutationFn: () => removeMember({ data: { org, member: member.did } }),
    onSuccess: invalidate,
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't remove member",
        description: e.message,
      }),
  });
  const toggle = (role: string) =>
    assign.mutate(
      member.roles.includes(role)
        ? member.roles.filter((r) => r !== role)
        : [...member.roles, role],
    );
  const name = member.displayName || member.handle || member.did;
  return (
    <TableRow>
      <TableCell className="max-w-0 pl-5">
        <div className="flex min-w-0 items-center gap-3">
          <UserAvatar
            actor={{
              did: member.did,
              handle: member.handle ?? undefined,
              displayName: member.displayName ?? undefined,
              avatar: member.avatarUrl ?? undefined,
            }}
            size="sm"
          />
          <div className="min-w-0">
            <p className="truncate font-medium">
              {name}
              {isMe && <span className="text-muted-foreground"> (you)</span>}
            </p>
            {member.handle && (
              <p className="truncate text-xs text-muted-foreground">
                @{member.handle}
              </p>
            )}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <RoleBadges ids={member.roles} roles={roles} />
      </TableCell>
      <TableCell className="hidden whitespace-nowrap text-muted-foreground md:table-cell">
        {member.updatedAt ? dateFormat.format(new Date(member.updatedAt)) : "—"}
      </TableCell>
      <TableCell className="pr-5 text-right">
        {(canAssign || (canEject && !isMe)) && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Manage ${name}`}
                />
              }
            >
              <MoreVertical />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-48">
              {canAssign && (
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Roles</DropdownMenuLabel>
                  {roles.map((r) => (
                    <DropdownMenuCheckboxItem
                      key={r.id}
                      checked={member.roles.includes(r.id)}
                      onCheckedChange={() => toggle(r.id)}
                    >
                      {r.name}
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuGroup>
              )}
              {canAssign && canEject && !isMe && <DropdownMenuSeparator />}
              {canEject && !isMe && (
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => remove.mutate()}
                >
                  <UserMinus />
                  Remove from group
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </TableCell>
    </TableRow>
  );
}

function RevokeButton({ org, id }: { org: string; id: string }) {
  const queryClient = useQueryClient();
  const revoke = useMutation({
    mutationFn: () => revokeInvite({ data: { org, id } }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["groupInvites", org] }),
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't revoke invite",
        description: e.message,
      }),
  });
  return (
    <Button
      variant="ghost"
      size="sm"
      disabled={revoke.isPending}
      onClick={() => revoke.mutate()}
    >
      Revoke
    </Button>
  );
}

function InviteDialog({ org, roles }: { org: string; roles: RoleView[] }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [invitee, setInvitee] = useState("");
  const [picked, setPicked] = useState<string[]>(["member"]);
  const invite = useMutation({
    mutationFn: () => inviteMember({ data: { org, invitee, roles: picked } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["groupInvites", org] });
      toast.add({ type: "success", title: `Invited ${invitee}` });
      setInvitee("");
      setOpen(false);
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't send invite",
        description: e.message,
      }),
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button />}>
        <MailPlus data-icon="inline-start" />
        Invite
      </DialogTrigger>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (invitee.trim()) invite.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>Invite someone</DialogTitle>
            <DialogDescription>
              They'll join with the roles you pick once they accept.
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="invitee">Handle or DID</FieldLabel>
            <Input
              id="invitee"
              autoFocus
              placeholder="alice.example.com"
              value={invitee}
              onChange={(e) => setInvitee(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>Roles</FieldLabel>
            <div className="flex flex-wrap gap-2">
              {(roles.length > 0
                ? roles
                : [{ id: "member", name: "Member", description: null }]
              ).map((r) => {
                const on = picked.includes(r.id);
                return (
                  <Button
                    key={r.id}
                    type="button"
                    size="sm"
                    variant={on ? "default" : "outline"}
                    onClick={() =>
                      setPicked((p) =>
                        on ? p.filter((x) => x !== r.id) : [...p, r.id],
                      )
                    }
                  >
                    {r.name}
                  </Button>
                );
              })}
            </div>
            <FieldDescription>
              With no roles picked, they join as a member.
            </FieldDescription>
          </Field>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              Cancel
            </DialogClose>
            <Button
              type="submit"
              disabled={!invitee.trim() || invite.isPending}
            >
              Send invite
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
