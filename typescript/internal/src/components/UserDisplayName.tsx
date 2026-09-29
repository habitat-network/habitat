import { Actor } from "@/types/Actor";

export interface UserDisplayNameProps {
  actor: Actor;
}

// UserDisplayName renders an actor's best available label: displayName, then
// handle, then the raw DID — the fallback chain used everywhere a single
// name string is shown for an actor.
export function UserDisplayName({ actor }: UserDisplayNameProps) {
  const { displayName, handle, did } = actor;
  return displayName || handle || did;
}
