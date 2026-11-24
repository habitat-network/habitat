import type { network } from "api";

// Known MCP connectors: servers Habitat knows how to configure, so admins
// only name them rather than fill in the generic custom server form. pear holds
// each connector's URL and OAuth client (see mcpgateway.Preset in Go).
// To add a connector, add a preset in Go and the lexicon, then append an entry
// to MCP_CONNECTORS; the picker and form are rendered from this list.

export interface McpConnector {
  // Stable identifier for the connector, used as a React key.
  id: string;
  // Brand mark, an SVG file in public/connectors served at
  // /connectors/<file>; shown in the add-server picker.
  icon: string;
  // Display name in the picker and form.
  name: string;
  description: string;
  // Default server name, which doubles as the server's ID and tool namespace
  // (see SERVER_NAME_PATTERN in McpServersEditor). Editable by the admin.
  defaultName: string;
  // The preset pear adds this connector with (mcpgateway.Preset in Go). pear
  // owns the connector's URL, auth type and OAuth client, which is shared by
  // all orgs, so none of them are sent from here.
  preset: network.habitat.mcp.addServer.$InputBody["preset"];
  // How each member connects afterward: signing in with OAuth, or adding their
  // own access token (mcpgateway.AuthType in Go).
  memberAuth: "oauth" | "token";
  // Link to the provider's own documentation.
  docsUrl: string;
}

export const MCP_CONNECTORS: McpConnector[] = [
  {
    id: "google-drive",
    icon: "google-drive.svg",
    preset: "google-drive",
    memberAuth: "oauth",
    name: "Google Drive",
    description: "Search and read files in each member's Google Drive.",
    defaultName: "google-drive",
    docsUrl:
      "https://developers.google.com/workspace/drive/api/guides/configure-mcp-server",
  },
  {
    id: "google-cloud",
    icon: "google-cloud.svg",
    preset: "google-cloud",
    memberAuth: "oauth",
    name: "Google Cloud",
    description:
      "Run gcloud commands against each member's Google Cloud projects.",
    defaultName: "google-cloud",
    docsUrl: "https://docs.cloud.google.com/sdk/use-gcloud-mcp",
  },
  {
    id: "google-calendar",
    icon: "google-calendar.svg",
    preset: "google-calendar",
    memberAuth: "oauth",
    name: "Google Calendar",
    description: "Search and read events in each member's Google Calendar.",
    defaultName: "google-calendar",
    docsUrl:
      "https://developers.google.com/workspace/calendar/api/guides/configure-mcp-server",
  },
  {
    id: "gmail",
    icon: "gmail.svg",
    preset: "gmail",
    memberAuth: "oauth",
    name: "Gmail",
    description: "Search and read email in each member's Gmail.",
    defaultName: "gmail",
    docsUrl: "https://developers.google.com/workspace/gmail/api/reference/mcp",
  },
  {
    id: "slack",
    icon: "slack.svg",
    preset: "slack",
    memberAuth: "oauth",
    name: "Slack",
    description: "Search and read messages, channels, and files in Slack.",
    defaultName: "slack",
    docsUrl: "https://docs.slack.dev/ai/slack-mcp-server/",
  },
  {
    id: "github",
    icon: "github.svg",
    preset: "github",
    memberAuth: "token",
    name: "GitHub",
    description: "Work with repositories, issues, and pull requests on GitHub.",
    defaultName: "github",
    docsUrl: "https://github.com/github/github-mcp-server",
  },
];
