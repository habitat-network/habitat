import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, type AuthManager } from "internal";
import { updateProfile, uploadOrgImage } from "@/queries/opensocial";
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
  Textarea,
} from "internal/components/ui";

export function EditProfileDialog({
  org,
  name,
  description,
  avatarUrl,
  authManager,
}: {
  org: string;
  name: string;
  description: string;
  avatarUrl?: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline">Edit</Button>} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit community profile</DialogTitle>
        </DialogHeader>
        {/* Unmounted while closed so each open starts from the latest
            fetched profile, without an effect to re-seed stale form state. */}
        {open && (
          <EditProfileForm
            org={org}
            initialName={name}
            initialDescription={description}
            avatarUrl={avatarUrl}
            authManager={authManager}
            onSaved={() => setOpen(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function EditProfileForm({
  org,
  initialName,
  initialDescription,
  avatarUrl,
  authManager,
  onSaved,
}: {
  org: string;
  initialName: string;
  initialDescription: string;
  avatarUrl?: string;
  authManager: AuthManager;
  onSaved: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(initialName);
  const [description, setDescription] = useState(initialDescription);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => updateProfile(authManager, org, name, description),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "profile", org],
      });
      onSaved();
    },
  });

  const {
    mutate: uploadAvatar,
    isPending: isUploadingAvatar,
    error: avatarError,
  } = useMutation({
    mutationFn: (file: File) => uploadOrgImage(authManager, org, file),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "profile", org],
      });
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (name.trim()) mutate();
      }}
    >
      <Field>
        <FieldLabel>Avatar</FieldLabel>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            disabled={isUploadingAvatar}
            className="rounded-full opacity-100 transition-opacity hover:opacity-80 disabled:opacity-50"
          >
            <OrgAvatar did={org} name={name} avatarUrl={avatarUrl} size="lg" />
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (file) uploadAvatar(file);
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={isUploadingAvatar}
            onClick={() => fileInputRef.current?.click()}
          >
            {isUploadingAvatar ? "Uploading…" : "Change avatar"}
          </Button>
        </div>
        <FieldError
          errors={avatarError ? [{ message: avatarError.message }] : []}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="org-name">Name</FieldLabel>
        <Input
          id="org-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="org-description">Description</FieldLabel>
        <Textarea
          id="org-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What is this community about?"
        />
      </Field>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <DialogFooter>
        <Button type="submit" disabled={isPending || !name.trim()}>
          {isPending ? "Saving…" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  );
}
