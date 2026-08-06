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

// parseSpaceURI splits a space URI into its components, returning null if the
// URI is malformed.
export function parseSpaceURI(uri: string): SpaceURIParts | null {
  if (uri.length > 8192) return null;
  const match = spaceURIRegex.exec(uri);
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
