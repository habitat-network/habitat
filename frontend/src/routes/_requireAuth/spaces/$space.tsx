import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { Controller, useForm } from "react-hook-form";
import { query, procedure, type AuthManager } from "internal";
import {
  Button,
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
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { X } from "lucide-react";
import { RecordRenderer } from "@/components/RecordRenderer";

export const Route = createFileRoute("/_requireAuth/spaces/$space")({
  async loader({ context, params }) {
    const { authManager } = context;
    const space = params.space;

    const { repos } = await query(
      "network.habitat.space.listRepos",
      { space },
      { fetcher: authManager },
    );

    const results = await Promise.all(
      repos.map(async (repo) => {
        const { records } = await query(
          "network.habitat.space.listRecords",
          { space, repo: repo.did },
          { fetcher: authManager },
        );
        return records.map((record) => ({ ...record, owner: repo.did }));
      }),
    );

    const records = results.flat();

    return { records, space, repos };
  },
  pendingComponent: () => {
    const { space } = Route.useParams();
    return <p>Loading records in {decodeURIComponent(space)}...</p>;
  },
  component: SpaceRecords,
});

function SpaceRecords() {
  const { space } = Route.useParams();
  const { records, repos } = Route.useLoaderData();
  const router = useRouter();
  const { authManager } = Route.useRouteContext();

  interface AddMemberForm {
    did: string;
    access: "read" | "write";
  }

  const form = useForm<AddMemberForm>({
    defaultValues: { did: "", access: "read" },
  });

  const { mutate: deleteRecord } = useMutation({
    async mutationFn({
      collection,
      rkey,
    }: {
      collection: string;
      rkey: string;
    }) {
      await procedure(
        "network.habitat.space.deleteRecord",
        { space, collection, rkey, repo: authManager.getAuthInfo()!.did },
        { fetcher: authManager },
      );
      router.invalidate();
    },
  });

  const { mutate: removeMember } = useMutation({
    async mutationFn(did: string) {
      await procedure(
        "network.habitat.space.removeMember",
        { space, did },
        { fetcher: authManager },
      );
      router.invalidate();
    },
  });

  const { mutate: addMember, isPending: isAdding } = useMutation({
    async mutationFn({ did, access }: AddMemberForm) {
      await procedure(
        "network.habitat.space.addMember",
        { space, did, access },
        { fetcher: authManager },
      );
      form.reset();
      router.invalidate();
    },
  });

  return (
    <>
      <h2 className="text-2xl mb-4">Space: {space}</h2>
      <p className="text-sm text-muted-foreground mb-4">
        {records.length} record{records.length !== 1 ? "s" : ""}
      </p>
      <CreateRecordDialog
        space={space}
        authManager={authManager}
        onCreated={() => router.invalidate()}
      />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead />
            <TableHead>Owner</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {records.map((record) => (
            <TableRow key={`${record.owner}-${record.rkey}`}>
              <TableCell>
                <div className="flex flex-col gap-2">
                  <span className="text-xs text-muted-foreground">
                    {record.collection} / {record.rkey}
                  </span>
                  <RecordRenderer
                    record={record.value ?? {}}
                    lexicon={record.collection}
                    uri={`${space}/${record.collection}/${record.rkey}`}
                  />
                </div>
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {record.owner}
              </TableCell>
              <TableCell>
                <Button
                  variant="destructive"
                  size="icon-xs"
                  aria-label="Delete record"
                  onClick={() =>
                    deleteRecord({
                      collection: record.collection,
                      rkey: record.rkey,
                    })
                  }
                >
                  <X />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <h3 className="text-xl mt-8 mb-2">Repos ({repos.length})</h3>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>DID</TableHead>
            <TableHead>Rev</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {repos.map((repo) => (
            <TableRow key={repo.did}>
              <TableCell className="font-mono text-xs">{repo.did}</TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {repo.rev ?? "-"}
              </TableCell>
              <TableCell>
                <Button
                  variant="destructive"
                  size="icon-xs"
                  aria-label="Remove member"
                  onClick={() => removeMember(repo.did)}
                >
                  <X />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <form
        onSubmit={form.handleSubmit((data) => addMember(data))}
        className="mt-6 flex items-end gap-3"
      >
        <Field>
          <FieldLabel>DID</FieldLabel>
          <Input
            {...form.register("did")}
            placeholder="did:plc:..."
            className="w-80"
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
        <Button disabled={isAdding} type="submit">
          Add Member
        </Button>
      </form>
    </>
  );
}

interface CreateRecordForm {
  collection: string;
  recordJson: string;
}

function CreateRecordDialog({
  space,
  authManager,
  onCreated,
}: {
  space: string;
  authManager: AuthManager;
  onCreated: () => void;
}) {
  const [open, setOpen] = useState(false);

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
        { fetcher: authManager },
      );
    },
    onSuccess() {
      form.reset();
      setOpen(false);
      onCreated();
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
