import { NetworkHabitatDocs, NetworkHabitatDocsEdit } from "api";
import { TypedRecord } from "internal";

export type DocRecord = TypedRecord<NetworkHabitatDocs.Main> & { isPublic: boolean };
export type DocEditRecord = TypedRecord<NetworkHabitatDocsEdit.Main>;
