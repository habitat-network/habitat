export type SearchParams = {
  q: string;
  limit?: number;
  cursor?: string;
};

type ResultView = {
  uri: string;
  recordType: string;
  snippet?: string;
};

type SearchOutput = {
  results: ResultView[];
  cursor?: string;
};

export async function searchHabitat(
  baseUrl: string,
  params: SearchParams
): Promise<string> {
  const url = new URL("/xrpc/network.habitat.search.query", baseUrl);
  url.searchParams.set("q", params.q);
  if (params.limit !== undefined) url.searchParams.set("limit", String(params.limit));
  if (params.cursor !== undefined) url.searchParams.set("cursor", params.cursor);

  const resp = await fetch(url.toString());
  if (!resp.ok) {
    throw new Error(`Search request failed: ${resp.status} ${resp.statusText}`);
  }

  const data = (await resp.json()) as SearchOutput;
  if (!Array.isArray(data.results)) {
    throw new Error("Unexpected response shape: missing results array");
  }
  return formatResults(params.q, data);
}

function formatResults(query: string, data: SearchOutput): string {
  if (data.results.length === 0) {
    return `No results found for "${query}".`;
  }

  const lines: string[] = [`Found ${data.results.length} results.\n`];

  for (let i = 0; i < data.results.length; i++) {
    const r = data.results[i];
    lines.push(`${i + 1}. ${r.recordType}`);
    lines.push(`   ${r.uri}`);
    if (r.snippet) lines.push(`   "${r.snippet}"`);
    lines.push("");
  }

  if (data.cursor) {
    lines.push(
      `Next cursor: ${data.cursor}  (pass as 'cursor' to get the next page)`
    );
  }

  return lines.join("\n").trimEnd();
}
