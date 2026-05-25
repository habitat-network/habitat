export function parseSpaceRecordUri(spaceUri: string) {
  const parts = spaceUri.split("/");
  return {
    spaceOwner: parts[2],
    spaceType: parts[3],
    spaceKey: parts[4],
    recordOwner: parts[5],
    recordCollection: parts[6],
    recordKey: parts[7],
  };
}
