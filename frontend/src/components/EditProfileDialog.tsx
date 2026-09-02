import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
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

interface FormValues {
  name: string;
  description: string;
  avatar: File | null;
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
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { register, handleSubmit, watch, setValue, reset, getValues } =
    useForm<FormValues>({
      defaultValues: {
        name: initialName,
        description: initialDescription,
        avatar: null,
      },
    });

  const avatar = watch("avatar");
  const watchedName = watch("name");
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);

  useEffect(() => {
    if (!avatar) {
      setPreviewUrl(null);
      return;
    }
    const url = URL.createObjectURL(avatar);
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [avatar]);

  const { mutate, isPending, error } = useMutation({
    mutationFn: async (values: FormValues) => {
      await updateProfile(authManager, org, values.name, values.description);
      if (values.avatar) {
        await uploadOrgImage(authManager, org, values.avatar);
      }
    },
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "profile", org],
      });
      reset();
      onSaved();
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={handleSubmit((values: FormValues) => mutate(values))}
    >
      <Field>
        <FieldLabel>Avatar</FieldLabel>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            className="rounded-full opacity-100 transition-opacity hover:opacity-80"
          >
            <OrgAvatar
              did={org}
              name={watchedName}
              avatarUrl={previewUrl ?? avatarUrl}
              size="lg"
            />
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (file) setValue("avatar", file);
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => fileInputRef.current?.click()}
          >
            Change avatar
          </Button>
        </div>
      </Field>
      <Field>
        <FieldLabel htmlFor="org-name">Name</FieldLabel>
        <Input id="org-name" {...register("name")} autoFocus />
      </Field>
      <Field>
        <FieldLabel htmlFor="org-description">Description</FieldLabel>
        <Textarea
          id="org-description"
          {...register("description")}
          placeholder="What is this community about?"
        />
      </Field>
      <FieldError
        errors={error ? [{ message: error.message }] : []}
      />
      <DialogFooter>
        <Button type="submit" disabled={isPending || !getValues("name").trim()}>
          {isPending ? "Saving…" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  );
}
