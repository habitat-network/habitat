import { SpaceRef, type DidString } from "@atproto/syntax";

// Recognizes a subject's shape, not an active group or a grant.
export function parseGroupSharingSubject(
  subject: string,
): { kind: "legacy" | "canonical"; orgDid: DidString } | undefined {
  let ref: SpaceRef;
  try {
    ref = SpaceRef.parse(subject);
  } catch {
    return undefined;
  }
  if (ref.spaceType === "community.opensocial.members") {
    return { kind: "legacy", orgDid: ref.spaceDid };
  }
  if (ref.spaceType === "org.atgrp.members" && ref.skey === "self") {
    return { kind: "canonical", orgDid: ref.spaceDid };
  }
  return undefined;
}
