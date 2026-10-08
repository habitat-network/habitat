export * from "./config";
export {
  Credentials,
  DelegationSource,
  type SpaceCredential,
} from "./Credentials";
export * from "./errors";
export { Identity, type ResolvedIdentity } from "./Identity";
export { type NotificationLxm, verifyNotification } from "./notification";
export {
  type CreateSpaceSyncerOptions,
  type PromiseDelegationSource,
  type PromiseRepoBatch,
  type PromiseSpaceSyncer,
  type PromiseSyncSink,
  type PromiseSyncStore,
  createSpaceSyncer,
} from "./promise";
export { SpaceClient, type SpaceCallError } from "./SpaceClient";
export { SpaceSyncer } from "./SpaceSyncer";
export { SyncSink } from "./SyncSink";
export { SyncStore } from "./SyncStore";
export * from "./types";
export {
  NotifySpaceDeletedInput,
  NotifyWriteInput,
  parseSpaceRef,
} from "./wire";
