import { useState } from "react";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
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
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { X } from "lucide-react";
import { spaceReposQueryOptions } from "@/queries/spaces";
import { SpacesBreadcrumb } from "@/components/SpacesBreadcrumb";
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute(
  "/_requireAuth/spaces/$spaceOwner/$spaceType/$spaceKey/",
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      spaceReposQueryOptions(constructSpaceURI(params), context.authManager),
    ),
  component: SpaceMembers,
});

function SpaceMembers() {
  const params = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const space = constructSpaceURI(params);
  const repos = Route.useLoaderData();

  const invalidateRepos = async () => {
    await queryClient.invalidateQueries(
      spaceReposQueryOptions(space, authManager),
    );
    await router.invalidate();
  };

  const { mutate: removeMember } = useMutation({
    async mutationFn(did: string) {
      await procedure(
        "network.habitat.space.removeMember",
        { space, did },
        { authManager },
      );
    },
    onSuccess: invalidateRepos,
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
      <div className="flex items-center justify-between">
        {/* Deliberately "Repos", not "Members": listRepos returns the writer
            set (repos that have written data), which is narrower than the
            space's ACL membership shown as memberCount on the type page. */}
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
              <TableHead className="w-0" />
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
                <TableCell>
                  <Button
                    variant="destructive"
                    size="icon-xs"
                    aria-label={`Remove ${repo.did}`}
                    onClick={() => removeMember(repo.did)}
                  >
                    <X />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <AddMemberForm
        space={space}
        authManager={authManager}
        onAdded={invalidateRepos}
      />
    </SpacesPageLayout>
  );
}

interface AddMemberForm {
  did: string;
  access: "read" | "write";
}

function AddMemberForm({
  space,
  authManager,
  onAdded,
}: {
  space: string;
  authManager: AuthManager;
  onAdded: () => void;
}) {
  const form = useForm<AddMemberForm>({
    defaultValues: { did: "", access: "read" },
  });

  const { mutate: addMember, isPending } = useMutation({
    async mutationFn({ did, access }: AddMemberForm) {
      await procedure(
        "network.habitat.space.addMember",
        { space, did, access },
        { authManager },
      );
    },
    onSuccess() {
      form.reset();
      onAdded();
    },
    onError(error) {
      toast.add({ title: "Couldn't add member", description: error.message });
    },
  });

  return (
    <form
      onSubmit={form.handleSubmit((data) => addMember(data))}
      className="flex items-end gap-3"
    >
      <Field>
        <FieldLabel>DID</FieldLabel>
        <Input
          {...form.register("did", { required: true })}
          placeholder="did:plc:…"
          className="w-80 font-mono"
        />
      </Field>
      <Field>
        <FieldLabel>Access</FieldLabel>
        <Controller
          control={form.control}
          name="access"
          render={({ field: { onChange, value, ...field } }) => (
            <ToggleGroup
              variant="outline"
              {...field}
              value={[value]}
              onValueChange={(v) => {
                if (v.length > 0) onChange(v[v.length - 1]);
              }}
            >
              <ToggleGroupItem value="read">Read</ToggleGroupItem>
              <ToggleGroupItem value="write">Write</ToggleGroupItem>
            </ToggleGroup>
          )}
        />
      </Field>
      <Button disabled={isPending} type="submit">
        Add member
      </Button>
    </form>
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
      // brand new member of the space.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["listRecords", space] }),
        queryClient.invalidateQueries({ queryKey: ["listRepos", space] }),
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
