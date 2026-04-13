export type HabitatDoc = {
  name: string;
  blob: string | null;
  editorClique?: string;
  isPublic?: boolean;
};

// Stored in network.habitat.docs.edit. `doc` backlinks to the original doc
// (habitat:// URI for private docs, at:// for public docs).
export type HabitatDocEdit = HabitatDoc & {
  doc: string;
};
