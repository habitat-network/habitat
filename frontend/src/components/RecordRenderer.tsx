import { useQuery } from "@tanstack/react-query";
import ReactJson from "react-json-view";
import { Badge } from "internal/components/ui";
import { renderSchemaQueryOptions } from "@/queries/renderSchema";
import type { FieldSchema } from "@/lib/renderSchemas";
import { ExternalLinkIcon } from "lucide-react";

const T = "network.habitat.render.schema";

// Parse an AT URI (at://<did>/<nsid>/<rkey>) into its components.
function parseAtUri(uri: string): { did: string; nsid: string; rkey: string } | null {
  const parts = uri.split("/");
  // ["at:", "", did, nsid, rkey]
  if (parts.length < 5) return null;
  return { did: parts[2], nsid: parts[3], rkey: parts[4] };
}

function expandLinkTemplate(template: string, uri: string): string | null {
  const parsed = parseAtUri(uri);
  if (!parsed) return null;
  const encodedHabitatUri = encodeURIComponent(
    `habitat://${parsed.did}/${parsed.nsid}/${parsed.rkey}`,
  );
  return template
    .replace("{encodedHabitatUri}", encodedHabitatUri)
    .replace("{did}", parsed.did)
    .replace("{nsid}", parsed.nsid)
    .replace("{rkey}", parsed.rkey);
}

function getNestedValue(obj: Record<string, unknown>, path: string): unknown {
  return path.split(".").reduce<unknown>((acc, key) => {
    if (acc !== null && typeof acc === "object") {
      return (acc as Record<string, unknown>)[key];
    }
    return undefined;
  }, obj);
}

function formatDatetime(value: unknown): string {
  if (typeof value !== "string") return String(value);
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));
  } catch {
    return value;
  }
}

// Extract the human-readable token name from an NSID#token string.
// e.g. "community.lexicon.calendar.event#scheduled" -> "scheduled"
function formatBadge(value: unknown): string {
  if (typeof value !== "string") return String(value);
  const hash = value.lastIndexOf("#");
  return hash >= 0 ? value.slice(hash + 1) : value;
}

function FieldValue({ field, value }: { field: FieldSchema; value: unknown }) {
  if (value === undefined || value === null || value === "") return null;

  switch (field.displayType) {
    case `${T}#datetime`:
      return <span>{formatDatetime(value)}</span>;

    case `${T}#url`:
      return (
        <a
          href={String(value)}
          target="_blank"
          rel="noopener noreferrer"
          className="underline text-primary"
        >
          {String(value)}
        </a>
      );

    case `${T}#badge`:
      return (
        <Badge variant="secondary">{formatBadge(value)}</Badge>
      );

    case `${T}#list`:
      if (!Array.isArray(value)) return <span>{String(value)}</span>;
      return (
        <ul className="list-disc list-inside space-y-0.5">
          {value.map((item, i) => (
            <li key={i} className="text-sm">
              {typeof item === "object" ? JSON.stringify(item) : String(item)}
            </li>
          ))}
        </ul>
      );

    default:
      return <span>{String(value)}</span>;
  }
}

function SchemaRenderer({
  record,
  fields,
  linkHref,
}: {
  record: Record<string, unknown>;
  fields: FieldSchema[];
  linkHref?: string;
}) {
  const primary = fields.filter((f) => f.priority === `${T}#primary`);
  const secondary = fields.filter((f) => f.priority === `${T}#secondary`);
  const metadata = fields.filter((f) => f.priority === `${T}#metadata`);

  function renderField(field: FieldSchema) {
    const value = getNestedValue(record, field.path);
    if (field.optional && (value === undefined || value === null || value === "")) {
      return null;
    }
    return (
      <div key={field.path}>
        <FieldValue field={field} value={value} />
      </div>
    );
  }

  function renderLabeledField(field: FieldSchema) {
    const value = getNestedValue(record, field.path);
    if (field.optional && (value === undefined || value === null || value === "")) {
      return null;
    }
    return (
      <div key={field.path} className="flex gap-2 items-baseline">
        <span className="text-sm text-muted-foreground shrink-0">{field.label}</span>
        <FieldValue field={field} value={value} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        {primary.map((f) => (
          <div key={f.path} className="font-medium">
            {renderField(f)}
          </div>
        ))}
        {linkHref && (
          <a
            href={linkHref}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground transition-colors"
            aria-label="Open record"
          >
            <ExternalLinkIcon size={14} />
          </a>
        )}
      </div>
      {secondary.length > 0 && (
        <div className="flex flex-col gap-1 text-sm">
          {secondary.map(renderLabeledField)}
        </div>
      )}
      {metadata.length > 0 && (
        <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
          {metadata.map(renderLabeledField)}
        </div>
      )}
    </div>
  );
}

export function RecordRenderer({
  record,
  lexicon,
  uri,
}: {
  record: Record<string, unknown>;
  lexicon: string;
  uri?: string;
}) {
  const { data: schema, isLoading } = useQuery(
    renderSchemaQueryOptions(lexicon),
  );

  if (isLoading) {
    return <ReactJson src={record} collapsed={1} />;
  }

  if (!schema) {
    return <ReactJson src={record} />;
  }

  const linkHref =
    schema.linkTemplate && uri
      ? expandLinkTemplate(schema.linkTemplate, uri) ?? undefined
      : undefined;

  return (
    <SchemaRenderer record={record} fields={schema.fields} linkHref={linkHref} />
  );
}
