import { createFileRoute } from "@tanstack/react-router";
import { LlmConnectionsEditor } from "@/components/LlmConnectionsEditor";
import { McpServersEditor } from "@/components/McpServersEditor";
import { useOrgStore } from "@/stores/org";

export const Route = createFileRoute("/_member/settings")({
  component: Settings,
});

// Settings is where an org's models and MCP servers are customised. Anyone
// can see them (and connect their own accounts); only members holding the
// configure actions get the add/edit/remove controls.
function Settings() {
  const org = useOrgStore((s) => s.selectedOrg);
  if (!org) return null;
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-10 overflow-y-auto p-6">
      <h1 className="text-xl font-semibold">Settings</h1>
      <McpServersEditor org={org} />
      <LlmConnectionsEditor org={org} />
    </div>
  );
}
