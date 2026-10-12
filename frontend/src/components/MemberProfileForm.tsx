import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { UserAvatar, type AuthManager } from "internal";
import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Textarea,
} from "internal/components/ui";
import { putMemberProfile, type MemberProfile } from "@/queries/opensocial";

interface FormValues {
  displayName: string;
  bio: string;
  avatarUrl: string;
}

// MemberProfileForm edits the signed-in user's own profile within an org
// (display name, bio, avatar URL): the community.opensocial.memberProfile
// record that getProfiles serves to the org's other members.
export function MemberProfileForm({
  org,
  did,
  initial,
  authManager,
}: {
  org: string;
  did: string;
  initial: MemberProfile;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(false);
  const { register, handleSubmit, watch } = useForm<FormValues>({
    defaultValues: {
      displayName: initial.displayName ?? "",
      bio: initial.bio ?? "",
      avatarUrl: initial.avatarUrl ?? "",
    },
  });
  const displayName = watch("displayName");
  const avatarUrl = watch("avatarUrl");

  const { mutate, isPending, error } = useMutation({
    mutationFn: (values: FormValues) =>
      putMemberProfile(authManager, org, values),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["opensocial", "memberProfile", org],
      });
      setSaved(true);
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={handleSubmit((values) => {
        setSaved(false);
        mutate(values);
      })}
    >
      <UserAvatar
        actor={{
          did,
          handle: did,
          displayName: displayName || undefined,
          avatar: avatarUrl || undefined,
        }}
      />
      <Field>
        <FieldLabel htmlFor="member-display-name">Display name</FieldLabel>
        <Input
          id="member-display-name"
          maxLength={256}
          {...register("displayName")}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="member-bio">Bio</FieldLabel>
        <Textarea
          id="member-bio"
          maxLength={2048}
          {...register("bio")}
          placeholder="A short bio"
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="member-avatar-url">Avatar URL</FieldLabel>
        <Input
          id="member-avatar-url"
          type="url"
          maxLength={2048}
          {...register("avatarUrl")}
          placeholder="https://example.com/me.png"
        />
      </Field>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving…" : "Save"}
        </Button>
        {saved && !isPending && (
          <span className="text-sm text-muted-foreground">Saved.</span>
        )}
      </div>
    </form>
  );
}
