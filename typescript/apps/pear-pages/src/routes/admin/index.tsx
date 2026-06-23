import { Button, Input } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";

// Instance admin settings page (migrated from internal/instance/home.html).
// Served same-origin under /ui/admin; all data is fetched from pear at runtime
// using the admin session cookie. If the session is missing the API calls
// return 401 and we send the admin to the login page.
export const Route = createFileRoute("/admin/")({
  component: AdminHomePage,
});

type Settings = { instanceName: string; orgCreationPolicy: string };

function AdminHomePage() {
  const [instanceName, setInstanceName] = useState("");
  const [policy, setPolicy] = useState("open");
  const [frontendDomain, setFrontendDomain] = useState("");
  const [inviteLink, setInviteLink] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  function show(setter: (v: string) => void, msg: string) {
    setError("");
    setSuccess("");
    setter(msg);
  }

  useEffect(() => {
    void fetch("/admin/config")
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error("config"))))
      .then((data: { frontendDomain: string }) =>
        setFrontendDomain(data.frontendDomain),
      )
      .catch(() => undefined);

    void fetch("/xrpc/network.habitat.admin.getSettings")
      .then((r) => {
        if (r.status === 401) {
          window.location.href = "/ui/admin/login";
          return Promise.reject(new Error("unauthenticated"));
        }
        if (!r.ok) return Promise.reject(new Error("load failed"));
        return r.json();
      })
      .then((data: Settings) => {
        setInstanceName(data.instanceName || "");
        setPolicy(data.orgCreationPolicy);
      })
      .catch((err: Error) => {
        if (err.message !== "unauthenticated") show(setError, "failed to load settings");
      });
  }, []);

  const onSave = () => {
    void fetch("/xrpc/network.habitat.admin.updateSettings", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ instanceName, orgCreationPolicy: policy }),
    })
      .then((r) => {
        if (!r.ok) throw new Error("save failed");
        return r.json();
      })
      .then((data: Settings) => {
        setPolicy(data.orgCreationPolicy);
        show(setSuccess, "Successfully saved settings");
      })
      .catch(() => show(setError, "failed to save settings"));
  };

  const onGenerateInvite = () => {
    void fetch("/xrpc/network.habitat.admin.issueInvite", { method: "POST" })
      .then((r) => {
        if (!r.ok) throw new Error("issue failed");
        return r.json();
      })
      .then((data: { token: string }) => {
        setInviteLink(
          "https://" + frontendDomain + "/org/create?token=" + data.token,
        );
      })
      .catch(() => show(setError, "failed to generate invite link"));
  };

  return (
    <main className="w-80 rounded-[0.625rem] border border-border bg-card p-8 shadow-sm">
      <h1 className="mb-1 text-xl font-semibold">Habitat</h1>
      <p className="mb-6 text-sm text-muted-foreground">Instance settings</p>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}
      {success && <p className="mb-4 text-sm text-green-700">{success}</p>}

      <label htmlFor="instanceName" className="mb-1.5 block text-sm font-medium">
        Instance name
      </label>
      <Input
        id="instanceName"
        className="mb-4"
        value={instanceName}
        onChange={(e) => setInstanceName(e.target.value)}
      />

      <label htmlFor="policy" className="mb-1.5 block text-sm font-medium">
        Org creation
      </label>
      <select
        id="policy"
        className="mb-4 w-full rounded-lg border border-border bg-background p-2 text-sm"
        value={policy}
        onChange={(e) => setPolicy(e.target.value)}
      >
        <option value="open">Open</option>
        <option value="invite_only">Invite only</option>
      </select>

      <Button className="w-full" onClick={onSave}>
        Save settings
      </Button>

      {policy === "invite_only" && (
        <div className="mt-4 flex flex-col gap-2">
          <Button variant="secondary" type="button" onClick={onGenerateInvite}>
            Generate invite link
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
