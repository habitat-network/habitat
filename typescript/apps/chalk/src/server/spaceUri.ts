// OutboxMessage is sap's wire format for a single outbox event, delivered as
// a webhook POST body (see cmd/sap/webhook.go webhookPayload). A JSON-null
// value is a delete tombstone: the record at uri was removed.
export interface OutboxMessage {
  id: number;
  uri: string;
  value: unknown;
}

export interface ParsedSpaceRecordUri {
  spaceUri: string;
  owner: string;
  type: string;
  skey: string;
  repo: string;
  collection: string;
}

// parseSpaceRecordUri splits a space-record URI
// (at://<owner>/space/<type>/<skey>/<repo>/<collection>/<rkey>, per
// internal/syntax.ConstructSpaceRecordURI) into its parts. Returns undefined
// if the URI isn't well-formed.
export function parseSpaceRecordUri(
  uri: string,
): ParsedSpaceRecordUri | undefined {
  if (!uri.startsWith("at://")) return undefined;
  const parts = uri.slice("at://".length).split("/");
  if (parts.length !== 7 || parts[1] !== "space") return undefined;
  const [owner, , type, skey, repo, collection] = parts;
  if (!owner || !type || !skey || !repo || !collection) return undefined;
  return {
    spaceUri: `at://${owner}/space/${type}/${skey}`,
    owner,
    type,
    skey,
    repo,
    collection,
  };
}
