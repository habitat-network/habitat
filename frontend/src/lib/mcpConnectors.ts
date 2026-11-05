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
  // The inputs the admin must (or may) supply. Empty for connectors that
  // need none.
  inputs: McpConnectorInput[];
  // Steps the admin must do outside Habitat before filling in the form.
  // {callbackUrl} is replaced with pear's MCP OAuth callback URL.
  setupSteps: string[];
  // Link to the provider's own documentation.
  docsUrl: string;
}

const clientIdInput: McpConnectorInput = {
  key: "clientId",
  label: "Client ID",
  required: true,
};

const clientSecretInput: McpConnectorInput = {
  key: "clientSecret",
  label: "Client secret",
  secret: true,
  required: true,
  help: "Stored encrypted, and never shown again.",
};

export const MCP_CONNECTORS: McpConnector[] = [
  {
    id: "google-drive",
    name: "Google Drive",
    description: "Search and read files in each member's Google Drive.",
    defaultName: "google-drive",
    url: "https://drivemcp.googleapis.com/mcp/v1",
    authType: "oauth",
    // Google doesn't support dynamic client registration, so an OAuth client
    // must be pre-registered.
    inputs: [clientIdInput, clientSecretInput],
    setupSteps: [
      "Join the Google Workspace Developer Preview Program and pick a Google Cloud project.",
      "Enable the drive.googleapis.com and drivemcp.googleapis.com APIs.",
      "Configure the OAuth consent screen and add the drive.readonly and drive.file scopes.",
      'Create an OAuth client of type "Web application", check "Use this client for an AI-powered agent", and add {callbackUrl} as an authorized redirect URI.',
    ],
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
    // Slack doesn't support dynamic client registration, and only allows
    // internal or Marketplace-published apps.
    inputs: [clientIdInput, clientSecretInput],
    setupSteps: [
      "Create a Slack app that is internal to your workspace or published in the Slack Marketplace (unlisted apps can't use the MCP server).",
      "Add {callbackUrl} as a redirect URL under OAuth & Permissions.",
      "Add the user token scopes for the tools you want members to use, such as search:read.public, channels:history and chat:write.",
      "Copy the app's client ID and client secret from Basic Information.",
    ],
    docsUrl: "https://docs.slack.dev/ai/slack-mcp-server/",
  },
  {
    id: "github",
    name: "GitHub",
    description: "Work with repositories, issues, and pull requests on GitHub.",
    defaultName: "github",
    url: "https://api.githubcopilot.com/mcp/",
    authType: "oauth",
    // GitHub's remote server requires each host to bring its own OAuth app or
    // GitHub App. It also accepts personal access tokens, but pear doesn't
    // support sending a static Authorization header yet.
    inputs: [clientIdInput, clientSecretInput],
    setupSteps: [
      "Create a GitHub OAuth App or GitHub App in your organization's developer settings.",
      "Set its authorization callback URL to {callbackUrl}.",
      "Copy the app's client ID and generate a client secret.",
    ],
    docsUrl: "https://github.com/github/github-mcp-server",
  },
];
