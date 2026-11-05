// Known MCP connectors: servers Habitat knows how to configure, so admins are
// asked only for the inputs each one needs rather than the generic custom
// server form. To add a connector, append an entry to MCP_CONNECTORS; the
// connector picker and its form are rendered from this list.

// The inputs a connector can ask the admin for. They map onto the fields of
// network.habitat.mcp.addServer; the server URL is fixed per connector, so it
// is never asked for.
export type McpConnectorInputKey = "clientId" | "clientSecret";

export interface McpConnectorInput {
  key: McpConnectorInputKey;
  label: string;
  // Rendered as a password input, for values stored encrypted and never shown
  // again.
  secret?: boolean;
  required: boolean;
  placeholder?: string;
  help?: string;
}

export interface McpConnector {
  // Stable identifier for the connector, used as a React key.
  id: string;
  // Display name in the picker and form.
  name: string;
  description: string;
  // Default server name, which doubles as the server's ID and tool namespace
  // (see SERVER_NAME_PATTERN in McpServersEditor). Editable by the admin.
  defaultName: string;
  // The server's streamable HTTP endpoint.
  url: string;
  // How pear authenticates to the server (mcpgateway.AuthType in Go).
  authType: "oauth" | "manual";
  // The inputs the admin must (or may) supply. Empty for connectors where
  // Habitat holds the OAuth client shared by all orgs (the case for every
  // connector below), so adding one only needs a name. A connector whose
  // provider needs a per-org OAuth client lists clientId and clientSecret.
  inputs: McpConnectorInput[];
  // Steps the admin must do outside Habitat before filling in the form.
  // {callbackUrl} is replaced with pear's MCP OAuth callback URL.
  setupSteps: string[];
  // Link to the provider's own documentation.
  docsUrl: string;
}

export const MCP_CONNECTORS: McpConnector[] = [
  {
    id: "google-drive",
    name: "Google Drive",
    description: "Search and read files in each member's Google Drive.",
    defaultName: "google-drive",
    url: "https://drivemcp.googleapis.com/mcp/v1",
    authType: "oauth",
    inputs: [],
    setupSteps: [],
    docsUrl:
      "https://developers.google.com/workspace/drive/api/guides/configure-mcp-server",
  },
  {
    id: "slack",
    name: "Slack",
    description: "Search and read messages, channels, and files in Slack.",
    defaultName: "slack",
    url: "https://mcp.slack.com/mcp",
    authType: "oauth",
    inputs: [],
    setupSteps: [],
    docsUrl: "https://docs.slack.dev/ai/slack-mcp-server/",
  },
  {
    id: "github",
    name: "GitHub",
    description: "Work with repositories, issues, and pull requests on GitHub.",
    defaultName: "github",
    url: "https://api.githubcopilot.com/mcp/",
    authType: "oauth",
    inputs: [],
    setupSteps: [],
    docsUrl: "https://github.com/github/github-mcp-server",
  },
];
