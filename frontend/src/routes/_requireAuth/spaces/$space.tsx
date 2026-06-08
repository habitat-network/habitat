import { useMutation } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { Controller, useForm } from "react-hook-form";
import { query, procedure } from "internal";
import {
  Button,
  Field,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { X } from "lucide-react";
import { RecordRenderer } from "@/components/RecordRenderer";

export const Route = createFileRoute("/_requireAuth/spaces/$space")({
  async loader({ context, params }) {
    const { authManager } = context;
    const space = params.space;

    const { members } = await query(
      "network.habitat.space.getMembers",
      { space },
      { authManager },
    );

    const results = await Promise.all(
      members.map(async (member) => {
        const { records } = await query(
          "network.habitat.space.listRecords",
          { space, repo: member.did },
          { authManager },
        );
        return records.map((record) => ({ ...record, owner: member.did }));
      }),
    );

    const records = results.flat();

    return { records, space, members };
  },
  pendingComponent: () => {
    const { space } = Route.useParams();
    return <p>Loading records in {decodeURIComponent(space)}...</p>;
  },
  component: SpaceRecords,
});

function SpaceRecords() {
  const { space } = Route.useParams();
  const { records, members } = Route.useLoaderData();
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
        { space, collection, rkey },
        { authManager },
      );
      router.invalidate();
    },
  });

  const { mutate: removeMember } = useMutation({
    async mutationFn(did: string) {
      await procedure(
        "network.habitat.space.removeMember",
        { space, did },
        { authManager },
      );
      router.invalidate();
    },
  });

  const { mutate: addMember, isPending: isAdding } = useMutation({
    async mutationFn({ did, access }: AddMemberForm) {
      await procedure(
        "network.habitat.space.addMember",
        { space, did, access },
        { authManager },
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
      <Button
        onClick={async () => {
          await procedure(
            "network.habitat.space.putRecord",
            {
              space,
              collection: "test.record.collection",
              record: { key: "value" },
            },
            { authManager },
          );
        }}
      >
        Create test record
      </Button>
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

      <h3 className="text-xl mt-8 mb-2">Members ({members.length})</h3>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>DID</TableHead>
            <TableHead>Access</TableHead>
            <TableHead>Added At</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((member) => (
            <TableRow key={member.did}>
              <TableCell className="font-mono text-xs">{member.did}</TableCell>
              <TableCell>{member.access ?? "read"}</TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {member.addedAt
                  ? new Date(member.addedAt).toLocaleString()
                  : "-"}
              </TableCell>
              <TableCell>
                <Button
                  variant="destructive"
                  size="icon-xs"
                  aria-label="Remove member"
                  onClick={() => removeMember(member.did)}
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
