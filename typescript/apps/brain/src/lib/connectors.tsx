import type { ComponentProps, ReactNode } from "react";
import type { network } from "api";

// Known MCP connectors: servers Habitat knows how to configure, so admins
// only name them rather than fill in the generic custom server form. pear
// holds each connector's URL and OAuth client (mcpgateway.Preset in Go).
// To add a connector, add a preset in Go and the lexicon, then append an entry
// here; the picker, form and server rows are rendered from this list.

export interface Connector {
  id: string;
  name: string;
  description: string;
  // Default server name, which doubles as the server's ID and tool namespace.
  defaultName: string;
  // The preset pear adds this connector with.
  preset: NonNullable<network.habitat.mcp.addServer.$InputBody["preset"]>;
  // How each member connects afterward.
  memberAuth: "oauth" | "token";
  docsUrl: string;
  // Hostname fragment of the connector's MCP endpoint, to recognise servers
  // an admin added under another name.
  hostHint: string;
  // Tile background behind the logo.
  tile: string;
  Logo: (props: ComponentProps<"svg">) => ReactNode;
}

function GoogleDriveLogo(props: ComponentProps<"svg">) {
  return (
    <svg viewBox="0 0 87.3 78" aria-hidden {...props}>
      <path
        d="m6.6 66.85 3.85 6.65c.8 1.4 1.95 2.5 3.3 3.3l13.75-23.8h-27.5c0 1.55.4 3.1 1.2 4.5z"
        fill="#0066da"
      />
      <path
        d="m43.65 25-13.75-23.8c-1.35.8-2.5 1.9-3.3 3.3l-25.4 44a9.06 9.06 0 0 0-1.2 4.5h27.5z"
        fill="#00ac47"
      />
      <path
        d="m73.55 76.8c1.35-.8 2.5-1.9 3.3-3.3l1.6-2.75 7.65-13.25c.8-1.4 1.2-2.95 1.2-4.5h-27.502l5.852 11.5z"
        fill="#ea4335"
      />
      <path
        d="m43.65 25 13.75-23.8c-1.35-.8-2.9-1.2-4.5-1.2h-18.5c-1.6 0-3.15.45-4.5 1.2z"
        fill="#00832d"
      />
      <path
        d="m59.8 53h-32.3l-13.75 23.8c1.35.8 2.9 1.2 4.5 1.2h50.8c1.6 0 3.15-.45 4.5-1.2z"
        fill="#2684fc"
      />
      <path
        d="m73.4 26.5-12.7-22c-.8-1.4-1.95-2.5-3.3-3.3l-13.75 23.8 16.15 28h27.45c0-1.55-.4-3.1-1.2-4.5z"
        fill="#ffba00"
      />
    </svg>
  );
}

function SlackLogo(props: ComponentProps<"svg">) {
  return (
    <svg viewBox="0 0 122.8 122.8" aria-hidden {...props}>
      <path
        d="M25.8 77.6c0 7.1-5.8 12.9-12.9 12.9S0 84.7 0 77.6s5.8-12.9 12.9-12.9h12.9v12.9zm6.5 0c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9v32.3c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V77.6z"
        fill="#e01e5a"
      />
      <path
        d="M45.2 25.8c-7.1 0-12.9-5.8-12.9-12.9S38.1 0 45.2 0s12.9 5.8 12.9 12.9v12.9H45.2zm0 6.5c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H12.9C5.8 58.1 0 52.3 0 45.2s5.8-12.9 12.9-12.9h32.3z"
        fill="#36c5f0"
      />
      <path
        d="M97 45.2c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9-5.8 12.9-12.9 12.9H97V45.2zm-6.5 0c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V12.9C64.7 5.8 70.5 0 77.6 0s12.9 5.8 12.9 12.9v32.3z"
        fill="#2eb67d"
      />
      <path
        d="M77.6 97c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9-12.9-5.8-12.9-12.9V97h12.9zm0-6.5c-7.1 0-12.9-5.8-12.9-12.9s5.8-12.9 12.9-12.9h32.3c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H77.6z"
        fill="#ecb22e"
      />
    </svg>
  );
}

// GitHub's mark is monochrome, so it follows the text colour and stays
// legible in dark mode.
function GitHubLogo(props: ComponentProps<"svg">) {
  return (
    <svg viewBox="0 0 16 16" fill="currentColor" aria-hidden {...props}>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

export const CONNECTORS: Connector[] = [
  {
    id: "google-drive",
    preset: "google-drive",
    memberAuth: "oauth",
    name: "Google Drive",
    description: "Search and read files in each member's Google Drive.",
    defaultName: "google-drive",
    docsUrl:
      "https://developers.google.com/workspace/drive/api/guides/configure-mcp-server",
    hostHint: "drive",
    tile: "bg-white ring-1 ring-black/10",
    Logo: GoogleDriveLogo,
  },
  {
    id: "slack",
    preset: "slack",
    memberAuth: "oauth",
    name: "Slack",
    description: "Search and read messages, channels, and files in Slack.",
    defaultName: "slack",
    docsUrl: "https://docs.slack.dev/ai/slack-mcp-server/",
    hostHint: "slack",
    tile: "bg-white ring-1 ring-black/10",
    Logo: SlackLogo,
  },
  {
    id: "github",
    preset: "github",
    memberAuth: "token",
    name: "GitHub",
    description: "Work with repositories, issues, and pull requests on GitHub.",
    defaultName: "github",
    docsUrl: "https://github.com/github/github-mcp-server",
    hostHint: "github",
    tile: "bg-white text-[#24292f] ring-1 ring-black/10 dark:bg-[#24292f] dark:text-white dark:ring-white/10",
    Logo: GitHubLogo,
  },
];

// connectorFor recognises a configured server as one of the known connectors,
// by its id (the default name) or its endpoint's host.
export function connectorFor(server: {
  id: string;
  url?: string;
}): Connector | undefined {
  const host = server.url ? URL.parse(server.url)?.hostname : undefined;
  return CONNECTORS.find(
    (c) =>
      c.defaultName === server.id.toLowerCase() ||
      (host !== undefined && host.includes(c.hostHint)),
  );
}

// ConnectorIcon is a connector's logo on its brand tile, or a generic plug
// for custom servers.
export function ConnectorIcon({
  connector,
  fallback,
  className = "size-9",
}: {
  connector: Connector | undefined;
  fallback: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={`flex shrink-0 items-center justify-center rounded-lg ${
        connector?.tile ?? "bg-muted text-muted-foreground"
      } ${className}`}
    >
      {connector ? <connector.Logo className="size-1/2" /> : fallback}
    </span>
  );
}
