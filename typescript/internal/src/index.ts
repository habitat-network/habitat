// Components
export { default as AuthForm } from "./AuthForm";
export { UserAvatar } from "./components/UserAvatar";
export type { UserAvatarProps } from "./components/UserAvatar";

// Managers and Sessions
export { AuthManager, UnauthenticatedError } from "./authManager";
export {
  HabitatClient,
  HabitatAgentSession,
  HabitatAuthedAgentSession,
  getAgent,
} from "./habitatClient";

// Utilities
export { default as clientMetadata } from "./clientMetadata";
export { default as reportWebVitals } from "./reportWebVitals";

// UI Components
export * from "./components/ui/button";
export * from "./components/ui/card";
export * from "./components/ui/dialog";
export * from "./components/ui/input";
export * from "./components/ui/input-group";
export * from "./components/ui/label";
export * from "./components/ui/radio-group";
export * from "./components/ui/textarea";

// Types
export type {
  CreateRecordResponse,
  GetRecordResponse,
  ListRecordsResponse,
  PutPrivateRecordResponse,
  GetPrivateRecordResponse,
  ListPrivateRecordsResponse,
  PutPrivateRecordInput,
  GetPrivateRecordParams,
  ListPrivateRecordsParams,
} from "./habitatClient";
