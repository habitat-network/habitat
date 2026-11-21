import { SpaceRef, type DidString } from "@atproto/syntax";
import { HabitatClient } from "./habitatClient";

// getSpaceBlob reads a blob (a doc's crdt state, an uploaded file, an app's
// code) out of a space through pear's network.habitat.space.getBlob, as the
// space's authority: the group, whose OAuth session the app holds once an
// admin has connected it. The group can read every space it owns, so no
// space credential is needed. The app keeps no copy of the bytes.
export async function getSpaceBlob(
  env: Env,
  space: string,
  repo: DidString,
  cid: string,
): Promise<Uint8Array> {
  const authority = SpaceRef.parse(space).spaceDid;
  return new HabitatClient(env, authority).getBlob(space, repo, cid);
}
