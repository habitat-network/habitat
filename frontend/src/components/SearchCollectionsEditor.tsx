import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import type { AuthManager } from "internal";
import type { DidString } from "@atproto/lex";
import { isValidNsid } from "@atproto/syntax";
import {
  addSearchCollectionMutationOptions,
  removeSearchCollectionMutationOptions,
  type SearchCollectionConfig,
} from "@/queries/searchConfig";
import {
  Badge,
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
} from "internal/components/ui";

interface AddCollectionFormValues {
  collection: string;
  // Comma-separated field paths.
  crawlableFields: string;
  filterableFields: string;
}

// parseFields splits a comma-separated list of field paths.
function parseFields(value: string): string[] {
  return value
    .split(",")
    .map((f) => f.trim())
    .filter((f) => f !== "");
}

// SearchCollectionsEditor lists the collections surfaced in the org's search
// results, and lets admins (canConfigure) add and remove them. Default
// collections are always included, so they're shown but can't be removed.
export function SearchCollectionsEditor({
  org,
  collections,
  defaults,
  canConfigure,
  authManager,
}: {
  org: DidString;
  collections: SearchCollectionConfig[];
  defaults: string[];
  canConfigure: boolean;
  authManager: AuthManager;
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">
          Searchable collections ({collections.length + defaults.length})
        </h2>
        {canConfigure && (
          <AddCollectionDialog org={org} authManager={authManager} />
        )}
      </div>
      <p className="text-sm text-muted-foreground">
        Search only returns records in these collections.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Collection</TableHead>
            <TableHead>Crawlable fields</TableHead>
            <TableHead>Filterable fields</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {defaults.map((collection) => (
            <TableRow key={collection}>
              <TableCell className="font-mono text-sm">{collection}</TableCell>
              <TableCell className="text-muted-foreground">All</TableCell>
              <TableCell className="text-muted-foreground">None</TableCell>
              <TableCell className="text-right">
                <Badge variant="secondary">Default</Badge>
              </TableCell>
            </TableRow>
          ))}
          {collections.map((config) => (
            <TableRow key={config.collection}>
              <TableCell className="font-mono text-sm">
                {config.collection}
              </TableCell>
              <TableCell className="font-mono text-sm text-muted-foreground">
                {config.crawlableFields?.join(", ") || "All"}
              </TableCell>
              <TableCell className="font-mono text-sm text-muted-foreground">
                {config.filterableFields?.join(", ") || "None"}
              </TableCell>
              <TableCell className="text-right">
                {canConfigure && (
                  <RemoveCollectionButton
                    org={org}
                    collection={config.collection}
                    authManager={authManager}
                  />
                )}
              </TableCell>
            </TableRow>
          ))}
          {collections.length === 0 && defaults.length === 0 && (
            <TableRow>
              <TableCell colSpan={4} className="text-muted-foreground">
                No collections are searchable yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}

function AddCollectionDialog({
  org,
  authManager,
}: {
  org: DidString;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  const {
    register,
    handleSubmit,
    reset: resetForm,
    formState: { errors, isValid },
  } = useForm<AddCollectionFormValues>({
    mode: "onChange",
    defaultValues: {
      collection: "",
      crawlableFields: "",
      filterableFields: "",
    },
  });

  const {
    mutate: add,
    isPending: adding,
    error,
    reset: resetMutation,
  } = useMutation(addSearchCollectionMutationOptions(authManager, org));

  const reset = () => {
    resetForm();
    resetMutation();
  };

  const submit = handleSubmit(
    ({ collection, crawlableFields, filterableFields }) => {
      const nsid = collection.trim();
      if (!isValidNsid(nsid)) return;
      add(
        {
          collection: nsid,
          crawlableFields: parseFields(crawlableFields),
          filterableFields: parseFields(filterableFields),
        },
        {
          onSuccess() {
            setOpen(false);
            reset();
          },
        },
      );
    },
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger render={<Button size="sm" />}>
        Add collection
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add a searchable collection</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={submit}>
          <Field>
            <FieldLabel htmlFor="search-collection">Collection</FieldLabel>
            <Input
              id="search-collection"
              placeholder="com.example.note"
              autoFocus
              {...register("collection", {
                required: "Enter a collection.",
                validate: (value) =>
                  isValidNsid(value.trim()) ||
                  "Enter a collection NSID, like com.example.note.",
              })}
            />
            <FieldError errors={[errors.collection]} />
            <p className="text-xs text-muted-foreground">
              Records in this collection will show up in search results for
              spaces this community owns.
            </p>
          </Field>
          <Field>
            <FieldLabel htmlFor="search-crawlable">
              Crawlable fields (optional)
            </FieldLabel>
            <Input
              id="search-crawlable"
              placeholder="title, body.text"
              {...register("crawlableFields")}
            />
            <p className="text-xs text-muted-foreground">
              Comma-separated paths of the fields whose text is searched. Leave
              empty to search every text field.
            </p>
          </Field>
          <Field>
            <FieldLabel htmlFor="search-filterable">
              Filterable fields (optional)
            </FieldLabel>
            <Input
              id="search-filterable"
              placeholder="author, status"
              {...register("filterableFields")}
            />
            <p className="text-xs text-muted-foreground">
              Comma-separated paths of the fields search results can be filtered
              by.
            </p>
          </Field>
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={adding || !isValid}>
              {adding ? "Adding…" : "Add collection"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RemoveCollectionButton({
  org,
  collection,
  authManager,
}: {
  org: DidString;
  collection: SearchCollectionConfig["collection"];
  authManager: AuthManager;
}) {
  const { mutate, isPending, error } = useMutation(
    removeSearchCollectionMutationOptions(authManager, org),
  );
  return (
    <div className="flex flex-col items-end gap-1">
      <Button
        variant="ghost"
        size="sm"
        disabled={isPending}
        onClick={() => mutate(collection)}
      >
        {isPending ? "Removing…" : "Remove"}
      </Button>
      <FieldError errors={error ? [{ message: error.message }] : []} />
    </div>
  );
}
