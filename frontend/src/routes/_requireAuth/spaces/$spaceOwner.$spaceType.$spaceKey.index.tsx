import { useState } from "react";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { constructSpaceURI, procedure, type AuthManager } from "internal";
import {
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
  toast,
} from "internal/components/ui";
import { X } from "lucide-react";
import {
  spaceMembersQueryOptions,
  spaceReposQueryOptions,
} from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/$spaceKey/",
)({
  async loader({ context, params }) {
    const space = constructSpaceURI(params);
    const [members, repos] = await Promise.all([
      context.queryClient.fetchQuery(
        spaceMembersQueryOptions(space, context.authManager),
      ),
      context.queryClient.fetchQuery(
        spaceReposQueryOptions(space, context.authManager),
      ),
    ]);
    return { members, repos };
  },
  component: SpaceMembers,
});

function SpaceMembers() {
  const params = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const space = constructSpaceURI(params);
  const { members, repos } = Route.useLoaderData();

  const invalidateMembers = async () => {
    await queryClient.invalidateQueries(
      spaceMembersQueryOptions(space, authManager),
    );
    await router.invalidate();
  };

  const { mutate: removeMember } = useMutation({
    async mutationFn(did: string) {
      await procedure(
        "network.habitat.simplespace.removeMember",
        { space, did },
        { authManager },
      );
    },
    onSuccess: invalidateMembers,
    onError(error) {
      toast.add({
        title: "Couldn't remove member",
        description: error.message,
      });
    },
  });

  return (
    <SpacesPageLayout
      breadcrumb={<SpacesBreadcrumb {...params} />}
      title={<span className="font-mono break-all">{params.spaceKey}</span>}
      subtitle={<span className="font-mono break-all">{space}</span>}
    >
      <section className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-medium">
            Members ({members.length})
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              accounts granted read access to this space
            </span>
          </h2>
          <AddMemberDialog
            space={space}
            authManager={authManager}
            onAdded={invalidateMembers}
          />
        </div>

        {members.length === 0 ? (
          <Card>
            <CardContent className="py-10 text-center text-muted-foreground">
              This space has no members yet.
            </CardContent>
          </Card>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>DID</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.did}>
                  <TableCell className="font-mono">{member.did}</TableCell>
                  <TableCell>
                    <Button
                      variant="destructive"
                      size="icon-xs"
                      aria-label={`Remove ${member.did}`}
                      onClick={() => removeMember(member.did)}
                    >
                      <X />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>

      <section className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          {/* Deliberately "Repos", not "Members": listRepos returns the writer
              set — the members who have actually written data — so it is a
              subset of the member list above. */}
          <h2 className="text-lg font-medium">
            Repos ({repos.length})
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              accounts holding data in this space
            </span>
          </h2>
          <CreateRecordDialog space={space} authManager={authManager} />
        </div>

        {repos.length === 0 ? (
          <Card>
            <CardContent className="py-10 text-center text-muted-foreground">
              No accounts hold data in this space yet.
            </CardContent>
          </Card>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>DID</TableHead>
                <TableHead>Rev</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {repos.map((repo) => (
                <TableRow key={repo.did}>
                  <TableCell className="font-mono">
                    <Link
                      to="/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner"
                      params={{ ...params, recordOwner: repo.did }}
                      className="hover:underline"
                      title={repo.did}
                    >
                      {repo.did}
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {repo.rev ?? "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>
    </SpacesPageLayout>
  );
}

interface AddMemberForm {
  did: string;
}

function AddMemberDialog({
  space,
  authManager,
  onAdded,
}: {
  space: string;
  authManager: AuthManager;
  onAdded: () => void;
}) {
  const [open, setOpen] = useState(false);

  const form = useForm<AddMemberForm>({
    defaultValues: { did: "" },
  });

  const {
    mutate: addMember,
    isPending,
    error: addError,
    reset: resetMutation,
  } = useMutation({
    async mutationFn({ did }: AddMemberForm) {
      await procedure(
        "network.habitat.simplespace.addMember",
        { space, did },
        { authManager },
      );
    },
    onSuccess() {
      form.reset();
      setOpen(false);
      onAdded();
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) {
          form.reset();
          resetMutation();
        }
      }}
    >
      <DialogTrigger render={<Button>Add member</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add member</DialogTitle>
        </DialogHeader>
        <form
          id="add-member-form"
          onSubmit={form.handleSubmit((data) => addMember(data))}
          className="flex flex-col gap-4"
        >
          <Field>
            <FieldLabel>DID</FieldLabel>
            <Input
              {...form.register("did", { required: "DID is required" })}
              placeholder="did:plc:…"
              className="font-mono"
            />
            <FieldError errors={[form.formState.errors.did]} />
          </Field>
          {/* The dialog stays open on failure so the reason lands next to the
              field that caused it — addMember rejects a DID the org doesn't
              know, and that is worth reading. */}
          {addError && <FieldError>{addError.message}</FieldError>}
        </form>
        <DialogFooter>
          <Button type="submit" form="add-member-form" disabled={isPending}>
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface CreateRecordForm {
  collection: string;
  recordJson: string;
}

function CreateRecordDialog({
  space,
  authManager,
}: {
  space: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const router = useRouter();

  const form = useForm<CreateRecordForm>({
    defaultValues: { collection: "", recordJson: "{\n  \n}" },
  });

  const {
    mutate: createRecord,
    isPending,
    error: createError,
    reset: resetMutation,
  } = useMutation({
    async mutationFn({ collection, recordJson }: CreateRecordForm) {
      let record: { [x: string]: unknown };
      try {
        record = JSON.parse(recordJson);
      } catch {
        throw new Error("Record must be valid JSON");
      }
      await procedure(
        "network.habitat.space.putRecord",
        { space, collection, record, repo: authManager.getAuthInfo()!.did },
        { authManager },
      );
    },
    async onSuccess() {
      form.reset();
      setOpen(false);
      // The new record lands in the caller's own repo, which may also be a
      // brand new member of the space. The keys are prefixes, so every repo's
      // records and commit under this space are covered.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["listRecords", space] }),
        queryClient.invalidateQueries({ queryKey: ["listRepos", space] }),
        queryClient.invalidateQueries({ queryKey: ["getLatestCommit", space] }),
      ]);
      await router.invalidate();
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) {
          form.reset();
          resetMutation();
        }
      }}
    >
      <DialogTrigger render={<Button>Create record</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create record</DialogTitle>
        </DialogHeader>
        <form
          id="create-record-form"
          onSubmit={form.handleSubmit((data) => createRecord(data))}
          className="flex flex-col gap-4"
        >
          <Field>
            <FieldLabel>Collection</FieldLabel>
            <Input
              {...form.register("collection", {
                required: "Collection is required",
              })}
              placeholder="network.habitat.example.thing"
              className="font-mono"
            />
            <FieldError errors={[form.formState.errors.collection]} />
          </Field>
          <Field>
            <FieldLabel>Record (JSON)</FieldLabel>
            <Textarea
              {...form.register("recordJson", {
                required: "Record is required",
              })}
              rows={10}
              className="font-mono"
            />
            <FieldError errors={[form.formState.errors.recordJson]} />
          </Field>
          {createError && <FieldError>{createError.message}</FieldError>}
        </form>
        <DialogFooter>
          <Button type="submit" form="create-record-form" disabled={isPending}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
