import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import {
  Button,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldError,
  FieldLabel,
  Input,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { Share2Icon } from "lucide-react";
import {
  docAccessQueryOptions,
  shareDoc,
  type ShareRole,
} from "@/queries/sharing";

interface ShareForm {
  did: string;
  role: ShareRole;
}

// ShareDialog lets the current user grant another user access to a doc by DID,
// as a viewer (read) or editor (read+write), by granting the corresponding role
// on the doc's space. It also lists who currently has access.
export function ShareDialog({
  space,
  authManager,
}: {
  space: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const access = useQuery({
    ...docAccessQueryOptions(space, authManager),
    enabled: open,
  });

  const form = useForm<ShareForm>({
    defaultValues: { did: "", role: "reader" },
  });

  const {
    mutate: share,
    isPending,
    error,
    reset: resetMutation,
  } = useMutation({
    mutationFn: ({ did, role }: ShareForm) =>
      shareDoc(authManager, space, did.trim(), role),
    onSuccess: async () => {
      form.reset();
      await queryClient.invalidateQueries(
        docAccessQueryOptions(space, authManager),
      );
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          form.reset();
          resetMutation();
        }
      }}
    >
      <DialogTrigger
        render={
          <Button size="icon" variant="outline" aria-label="Share document">
            <Share2Icon />
          </Button>
        }
      />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Share document</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={form.handleSubmit((data) => share(data))}
          className="flex flex-col gap-4"
        >
          <Field>
            <FieldLabel>User DID</FieldLabel>
            <Input
              {...form.register("did", { required: "A DID is required" })}
              placeholder="did:web:..."
              className="font-mono"
            />
            <FieldError errors={[form.formState.errors.did]} />
          </Field>
          <Field>
            <FieldLabel>Access</FieldLabel>
            <Controller
              control={form.control}
              name="role"
              render={({ field: { onChange, value, ...field } }) => (
                <ToggleGroup
                  variant="outline"
                  {...field}
                  value={[value]}
                  onValueChange={(v) => {
                    if (v.length > 0) onChange(v[v.length - 1]);
                  }}
                >
                  <ToggleGroupItem value="reader">Viewer</ToggleGroupItem>
                  <ToggleGroupItem value="writer">Editor</ToggleGroupItem>
                </ToggleGroup>
              )}
            />
          </Field>
          {error && <FieldError>{(error as Error).message}</FieldError>}
          <Button type="submit" disabled={isPending}>
            {isPending ? "Sharing…" : "Share"}
          </Button>
        </form>

        <div className="mt-2 flex flex-col gap-3 text-sm">
          <AccessList
            title="Editors"
            dids={access.data?.editors ?? []}
            loading={access.isLoading}
          />
          <AccessList
            title="Viewers"
            dids={access.data?.viewers ?? []}
            loading={access.isLoading}
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

function AccessList({
  title,
  dids,
  loading,
}: {
  title: string;
  dids: string[];
  loading: boolean;
}) {
  return (
    <div>
      <p className="font-medium">{title}</p>
      {loading ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : dids.length === 0 ? (
        <p className="text-muted-foreground">None</p>
      ) : (
        <ul className="font-mono text-xs text-muted-foreground">
          {dids.map((did) => (
            <li key={did}>{did}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
