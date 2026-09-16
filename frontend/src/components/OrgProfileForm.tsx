import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, type AuthManager } from "internal";
import { updateProfile, uploadOrgImage } from "@/queries/opensocial";
import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Textarea,
} from "internal/components/ui";

interface FormValues {
  name: string;
  description: string;
  avatar: File | null;
}

// OrgProfileForm edits a community's profile (name, description, avatar) in
// place. Used as the body of the org settings page.
export function OrgProfileForm({
  org,
  initialName,
  initialDescription,
  avatarUrl,
  authManager,
}: {
  org: string;
  initialName: string;
  initialDescription: string;
  avatarUrl?: string;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { register, handleSubmit, watch, setValue, getValues } =
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
  const [saved, setSaved] = useState(false);

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
      setValue("avatar", null);
      setSaved(true);
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={handleSubmit((values: FormValues) => {
        setSaved(false);
        mutate(values);
      })}
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
        <Input id="org-name" {...register("name")} />
      </Field>
      <Field>
        <FieldLabel htmlFor="org-description">Description</FieldLabel>
        <Textarea
          id="org-description"
          {...register("description")}
          placeholder="What is this community about?"
        />
      </Field>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={isPending || !getValues("name").trim()}>
          {isPending ? "Saving…" : "Save"}
        </Button>
        {saved && !isPending && (
          <span className="text-sm text-muted-foreground">Saved.</span>
        )}
      </div>
    </form>
  );
}
