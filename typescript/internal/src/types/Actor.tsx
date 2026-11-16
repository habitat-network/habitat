export interface Actor {
  did: string;
  handle?: string;
  displayName?: string;
  avatar?: string;
}

// actorLabel returns an actor's best available name: displayName, then
// handle, then the raw DID. Blank or whitespace-only values count as missing,
// so profiles that are absent or only partially filled in still get a label.
export function actorLabel({ displayName, handle, did }: Actor): string {
  return displayName?.trim() || handle?.trim() || did;
}
