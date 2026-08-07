// Habitat-specific syntax types, mirroring the Go implementation in
// internal/syntax. Keep the accepted grammars in sync with that package.

// SpaceURIParts are the three components of a space URI:
// "at://<owner did>/space/<type nsid>/<space key>".
export interface SpaceURIParts {
  spaceOwner: string;
  spaceType: string;
  spaceKey: string;
}

const spaceURIRegex =
  /^at:\/\/([a-zA-Z0-9._:%-]+)\/space\/([a-zA-Z0-9-.]+)\/([a-zA-Z0-9_~.:-]{1,512})$/;

// The pre-0016 format: ats://<did>/<type>/<skey>. Still accepted when parsing so
// URIs held by older clients keep resolving, but never produced by
// constructSpaceURI.
const legacySpaceURIRegex =
  /^ats:\/\/([a-zA-Z0-9._:%-]+)\/([a-zA-Z0-9-.]+)\/([a-zA-Z0-9_~.:-]{1,512})$/;

// parseSpaceURI splits a space URI into its components, returning null if the
// URI is malformed. Both the current and legacy formats are accepted.
export function parseSpaceURI(uri: string): SpaceURIParts | null {
  if (uri.length > 8192) return null;
  const match = spaceURIRegex.exec(uri) ?? legacySpaceURIRegex.exec(uri);
  if (!match) return null;
  const [, spaceOwner, spaceType, spaceKey] = match;
  return { spaceOwner, spaceType, spaceKey };
}

// constructSpaceURI assembles a space URI from its components.
export function constructSpaceURI({
  spaceOwner,
  spaceType,
  spaceKey,
}: SpaceURIParts): string {
  return `at://${spaceOwner}/space/${spaceType}/${spaceKey}`;
}
