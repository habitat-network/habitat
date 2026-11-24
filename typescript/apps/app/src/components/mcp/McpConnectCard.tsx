import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  CodeBlock,
  CodeBlockActions,
  CodeBlockContent,
  CodeBlockCopyButton,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "internal/components/ui";

// McpConnectCard shows the org's MCP URL and how to add it to the MCP clients
// members use. Instructions follow each client's own remote-MCP setup.

const SERVER_NAME = "habitat";

interface ClientGuide {
  id: string;
  name: string;
  // Steps before the snippet, if any.
  steps: string[];
  // Optional config or command to copy.
  snippet?: (url: string) => string;
  snippetLabel?: string;
}

const CLIENTS: ClientGuide[] = [
  {
    id: "claude-code",
    name: "Claude Code",
    steps: [
      "Run this command in your terminal.",
      "Start Claude Code, run /mcp, and choose habitat to sign in.",
    ],
    snippet: (url) => `claude mcp add --transport http ${SERVER_NAME} ${url}`,
  },
  {
    id: "claude",
    name: "Claude",
    steps: [
      "Open Settings, then Connectors (Customize, then Connectors in Claude Desktop).",
      "Choose Add custom connector, name it Habitat, and paste the URL above.",
      "Click Connect and sign in.",
    ],
  },
  {
    id: "cursor",
    name: "Cursor",
    steps: [
      "Open Settings, then Tools & MCP, and choose Add Custom MCP. Add this to mcp.json.",
      "Click Connect next to habitat and sign in.",
    ],
    snippetLabel: "mcp.json",
    snippet: (url) =>
      JSON.stringify({ mcpServers: { [SERVER_NAME]: { url } } }, null, 2),
  },
  {
    id: "vscode",
    name: "VS Code",
    steps: [
      "Run MCP: Add Server from the command palette, or add this to .vscode/mcp.json.",
      "Click Start above the server entry and sign in.",
    ],
    snippetLabel: ".vscode/mcp.json",
    snippet: (url) =>
      JSON.stringify(
        { servers: { [SERVER_NAME]: { type: "http", url } } },
        null,
        2,
      ),
  },
  {
    id: "codex",
    name: "Codex",
    steps: ["Add the server, then sign in with the second command."],
    snippet: (url) =>
      `codex mcp add ${SERVER_NAME} --url ${url}\ncodex mcp login ${SERVER_NAME}`,
  },
];

function Snippet({ value, label }: { value: string; label?: string }) {
  return (
    <CodeBlock size="sm">
      <CodeBlockActions>
        <CodeBlockCopyButton value={value} />
      </CodeBlockActions>
      <CodeBlockContent aria-label={label}>{value}</CodeBlockContent>
    </CodeBlock>
  );
}

export function McpConnectCard({ url }: { url: string }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>MCP server URL</CardTitle>
        <CardDescription>
          Add this URL to an MCP client to use your group&apos;s data and
          connected servers.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Snippet value={url} label="MCP server URL" />
        <Tabs defaultValue={CLIENTS[0].id}>
          <TabsList className="max-w-full overflow-x-auto">
            {CLIENTS.map((c) => (
              <TabsTrigger key={c.id} value={c.id}>
                {c.name}
              </TabsTrigger>
            ))}
          </TabsList>
          {CLIENTS.map((c) => (
            <TabsContent
              key={c.id}
              value={c.id}
              className="flex flex-col gap-3"
            >
              <ol className="flex list-decimal flex-col gap-1 pl-5">
                {c.steps.map((step) => (
                  <li key={step}>{step}</li>
                ))}
              </ol>
              {c.snippet && (
                <Snippet value={c.snippet(url)} label={c.snippetLabel} />
              )}
            </TabsContent>
          ))}
        </Tabs>
      </CardContent>
    </Card>
  );
}
