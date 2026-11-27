import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Field,
  FieldLabel,
  Input,
  Textarea,
  toast,
} from "internal/components/ui";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import {
  groupLabel,
  groupQuery,
  myGroupProfileQuery,
  myGroupsQuery,
} from "@/lib/queries/groups";
import {
  leaveGroup,
  putMyGroupProfile,
  updateGroupProfile,
} from "@/server/fns/groups";

// Group settings: the group's public profile (for community.configure),
// the member's own profile within the group, and leaving it.

export const Route = createFileRoute("/_member/g/$org/settings")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(groupQuery(params.org)),
      context.queryClient.ensureQueryData(myGroupProfileQuery(params.org)),
    ]),
  component: SettingsPage,
});

function SettingsPage() {
  const { org } = Route.useParams();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  return (
    <>
      <PageHeader title="Group settings" description={groupLabel(group)} />
      <PageBody className="flex max-w-3xl flex-col gap-6">
        <GroupProfileCard
          org={org}
          initial={{
            name: group.name ?? "",
            description: group.description ?? "",
          }}
          canEdit={group.myActions.includes("community.configure")}
        />
        <MyProfileCard org={org} />
        <LeaveCard org={org} name={groupLabel(group)} />
      </PageBody>
    </>
  );
}

function GroupProfileCard({
  org,
  initial,
  canEdit,
}: {
  org: string;
  initial: { name: string; description: string };
  canEdit: boolean;
}) {
  const queryClient = useQueryClient();
  const {
    register,
    handleSubmit,
    reset,
    formState: { isValid },
  } = useForm<typeof initial>({ mode: "onChange", defaultValues: initial });
  const save = useMutation({
    mutationFn: (v: typeof initial) =>
      updateGroupProfile({ data: { org, ...v } }),
    onSuccess: async (_, v) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["group", org] }),
        queryClient.invalidateQueries({ queryKey: myGroupsQuery.queryKey }),
      ]);
      reset(v);
      toast.add({ type: "success", title: "Saved the group's profile" });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't save",
        description: e.message,
      }),
  });
  return (
    <form onSubmit={handleSubmit((v) => save.mutate(v))}>
      <Card>
        <CardHeader>
          <CardTitle>Profile</CardTitle>
          <CardDescription>
            What people see when they look the group up.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Field>
            <FieldLabel htmlFor="group-name">Name</FieldLabel>
            <Input
              id="group-name"
              disabled={!canEdit}
              {...register("name", { validate: (v) => v.trim() !== "" })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="group-description">Description</FieldLabel>
            <Textarea
              id="group-description"
              disabled={!canEdit}
              {...register("description")}
            />
          </Field>
        </CardContent>
        {canEdit && (
          <CardFooter className="justify-end">
            <Button type="submit" disabled={save.isPending || !isValid}>
              Save profile
            </Button>
          </CardFooter>
        )}
      </Card>
    </form>
  );
}

function MyProfileCard({ org }: { org: string }) {
  const { data } = useSuspenseQuery(myGroupProfileQuery(org));
  return <MyProfileForm key={org} org={org} initial={data} />;
}

type MyProfileValues = { displayName: string; bio: string };

function MyProfileForm({
  org,
  initial,
}: {
  org: string;
  initial: { displayName: string; bio: string; handle: string | null };
}) {
  const queryClient = useQueryClient();
  const { register, handleSubmit, reset } = useForm<MyProfileValues>({
    defaultValues: { displayName: initial.displayName, bio: initial.bio },
  });
  const save = useMutation({
    mutationFn: (v: MyProfileValues) =>
      putMyGroupProfile({ data: { org, ...v } }),
    onSuccess: async (_, v) => {
      reset(v);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["myGroupProfile", org] }),
        queryClient.invalidateQueries({ queryKey: ["group", org] }),
        queryClient.invalidateQueries({ queryKey: ["members", org] }),
      ]);
      toast.add({ type: "success", title: "Saved your profile" });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't save",
        description: e.message,
      }),
  });
  return (
    <form onSubmit={handleSubmit((v) => save.mutate(v))}>
      <Card id="profile">
        <CardHeader>
          <CardTitle>Your profile in this group</CardTitle>
          <CardDescription>
            Other members see this instead of your public profile.
            {initial.handle && ` You're @${initial.handle}.`}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Field>
            <FieldLabel htmlFor="my-name">Display name</FieldLabel>
            <Input id="my-name" {...register("displayName")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="my-bio">Bio</FieldLabel>
            <Textarea id="my-bio" {...register("bio")} />
          </Field>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" disabled={save.isPending}>
            Save
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}

function LeaveCard({ org, name }: { org: string; name: string }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const leave = useMutation({
    mutationFn: () => leaveGroup({ data: { org } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: myGroupsQuery.queryKey });
      await navigate({ to: "/" });
      // After leaving the page: its suspense queries would refetch (and
      // fail with NotAMember) if the group went away while it was mounted.
      queryClient.removeQueries({ queryKey: ["group", org] });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't leave the group",
        description: e.message,
      }),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Leave {name}</CardTitle>
        <CardDescription>
          You'll lose access to its docs and files. Someone would need to invite
          you again.
        </CardDescription>
      </CardHeader>
      <CardFooter className="justify-end gap-2">
        {confirming && (
          <Button variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
        )}
        <Button
          variant="destructive"
          disabled={leave.isPending}
          onClick={() => (confirming ? leave.mutate() : setConfirming(true))}
        >
          {confirming ? "Yes, leave the group" : "Leave group"}
        </Button>
      </CardFooter>
    </Card>
  );
}
