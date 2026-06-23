import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import {
  Button,
  Field,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute, redirect } from "@tanstack/react-router";

// Instance admin settings page (migrated from internal/instance/home.html).
// Served same-origin under /ui/admin; all data is fetched from pear at runtime
// using the admin session cookie. If the session is missing the API calls
// return 401 and we send the admin to the login page.
type Settings = { instanceName: string; orgCreationPolicy: string };
type LoaderData = Settings & { frontendDomain: string };
type FormValues = { instanceName: string; orgCreationPolicy: string };

export const Route = createFileRoute("/admin/")({
  loader: async (): Promise<LoaderData> => {
    const [configRes, settingsRes] = await Promise.all([
      fetch("/admin/config"),
      fetch("/xrpc/network.habitat.admin.getSettings"),
    ]);

    if (settingsRes.status === 401) {
      throw redirect({ to: "/admin/login", search: { error: "" } });
    }
    if (!configRes.ok || !settingsRes.ok) {
      throw new Error("Failed to load settings");
    }

    const config = (await configRes.json()) as { frontendDomain: string };
    const settings = (await settingsRes.json()) as Settings;
    return { ...settings, ...config };
  },
  component: AdminHomePage,
});

function AdminHomePage() {
  const { instanceName, orgCreationPolicy, frontendDomain } = Route.useLoaderData();
  const [success, setSuccess] = useState("");
  const [inviteLink, setInviteLink] = useState("");

  const { register, handleSubmit } = useForm<FormValues>({
    defaultValues: { instanceName, orgCreationPolicy },
  });

  const saveMutation = useMutation({
    mutationFn: async (data: FormValues) => {
      const res = await fetch("/xrpc/network.habitat.admin.updateSettings", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(data),
      });
      if (!res.ok) throw new Error("Failed to save settings");
    },
    onSuccess: () => setSuccess("Successfully saved settings"),
  });

  const inviteMutation = useMutation({
    mutationFn: async () => {
      const res = await fetch("/xrpc/network.habitat.admin.issueInvite", {
        method: "POST",
      });
      if (!res.ok) throw new Error("Failed to generate invite link");
      const data = (await res.json()) as { token: string };
      setInviteLink(
        "https://" + frontendDomain + "/org/create?token=" + data.token,
      );
    },
  });

  const error = saveMutation.error?.message || inviteMutation.error?.message;

  return (
    <main className="w-80 rounded-[0.625rem] border border-border bg-card p-8 shadow-sm">
      <h1 className="mb-1 text-xl font-semibold">Habitat</h1>
      <p className="mb-6 text-sm text-muted-foreground">Instance settings</p>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}
      {success && <p className="mb-4 text-sm text-green-700">{success}</p>}

      <form onSubmit={handleSubmit((data) => saveMutation.mutate(data))}>
        <fieldset disabled={saveMutation.isPending} className="flex flex-col gap-4">
          <Field>
            <FieldLabel htmlFor="instanceName">Instance name</FieldLabel>
            <Input
              id="instanceName"
              {...register("instanceName")}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="policy">Org creation</FieldLabel>
            <select
              id="policy"
              className="w-full rounded-lg border border-border bg-background p-2 text-sm"
              {...register("orgCreationPolicy")}
            >
              <option value="open">Open</option>
              <option value="invite_only">Invite only</option>
            </select>
          </Field>

          <Button type="submit">
            {saveMutation.isPending ? "Saving..." : "Save settings"}
          </Button>
        </fieldset>
      </form>

      {orgCreationPolicy === "invite_only" && (
        <div className="mt-4 flex flex-col gap-2">
          <Button
            variant="secondary"
            type="button"
            onClick={() => inviteMutation.mutate()}
            disabled={inviteMutation.isPending}
          >
            {inviteMutation.isPending ? "Generating..." : "Generate invite link"}
          </Button>
          {inviteLink && <Input readOnly value={inviteLink} />}
        </div>
      )}

      <form method="POST" action="/admin/logout" className="mt-6">
        <Button variant="secondary" type="submit" className="w-full">
          Log out
        </Button>
      </form>
    </main>
  );
}
