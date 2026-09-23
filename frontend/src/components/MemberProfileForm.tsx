import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, type AuthManager } from "internal";
import { updateMemberProfile } from "@/queries/org";
import {
  Button,
  Field,
  FieldLabel,
  Input,
  Textarea,
} from "internal/components/ui";

interface FormValues {
  displayName: string;
  bio: string;
  avatarUrl: string;
}

// MemberProfileForm edits the caller's own profile (display name, bio,
// avatar URL) within org. Used as the body of the member's "My profile" page.
export function MemberProfileForm({
  org,
  did,
  initialDisplayName,
  initialBio,
  initialAvatarUrl,
  authManager,
}: {
  org: string;
  did: string;
  initialDisplayName: string;
  initialBio: string;
  initialAvatarUrl: string;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();

  const { register, handleSubmit, watch } = useForm<FormValues>({
    defaultValues: {
      displayName: initialDisplayName,
      bio: initialBio,
      avatarUrl: initialAvatarUrl,
    },
  });

  const watchedName = watch("displayName");
  const watchedAvatarUrl = watch("avatarUrl");

  const { mutate, isPending, error, isSuccess } = useMutation({
    mutationFn: (values: FormValues) =>
      updateMemberProfile(
        authManager,
        org,
        values.displayName,
        values.bio,
        values.avatarUrl,
      ),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["org", "profile", did],
      });
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={handleSubmit((values) => mutate(values))}
    >
      <Field>
        <FieldLabel>Avatar</FieldLabel>
        <div className="flex items-center gap-3">
          <OrgAvatar
            did={did}
            name={watchedName}
            avatarUrl={watchedAvatarUrl || undefined}
            size="lg"
          />
        </div>
      </Field>
      <Field>
        <FieldLabel htmlFor="member-avatar-url">Avatar URL</FieldLabel>
        <Input
          id="member-avatar-url"
          placeholder="https://…"
          {...register("avatarUrl")}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="member-display-name">Display name</FieldLabel>
        <Input id="member-display-name" {...register("displayName")} />
      </Field>
      <Field>
        <FieldLabel htmlFor="member-bio">Bio</FieldLabel>
        <Textarea
          id="member-bio"
          {...register("bio")}
          placeholder="A short bio for your teammates."
        />
      </Field>
      {error && (
        <p className="text-sm text-destructive">{(error as Error).message}</p>
      )}
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving…" : "Save"}
        </Button>
        {isSuccess && !isPending && (
          <span className="text-sm text-muted-foreground">Saved.</span>
        )}
      </div>
    </form>
  );
}
