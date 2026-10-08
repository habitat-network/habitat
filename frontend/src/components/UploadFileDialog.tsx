import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import { uploadOrgFile } from "@/queries/opensocial";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";

// UploadFileDialog uploads a file into a new space shared with every member
// of the org.
export function UploadFileDialog({
  org,
  authManager,
}: {
  org: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const queryClient = useQueryClient();

  const { mutate, isPending, error, reset } = useMutation({
    mutationFn: (f: File) => uploadOrgFile(authManager, org, f),
    async onSuccess() {
      // A new space also belongs in the cached space listings.
      await queryClient.invalidateQueries({ queryKey: ["listSpaces"] });
      setOpen(false);
      setFile(null);
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setFile(null);
          reset();
        }
      }}
    >
      <DialogTrigger render={<Button>Upload file</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Upload a file</DialogTitle>
          <DialogDescription>
            The file is stored in a new space, shared with all members of this
            organization.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (file) mutate(file);
          }}
        >
          <Field>
            <FieldLabel>File</FieldLabel>
            <Input
              type="file"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
            <FieldError errors={error ? [{ message: error.message }] : []} />
          </Field>
          <DialogFooter>
            <Button type="submit" disabled={isPending || !file}>
              {isPending ? "Uploading…" : "Upload"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
