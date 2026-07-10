// Components
export { default as AuthForm } from "./AuthForm";
export { UserAvatar } from "./components/UserAvatar";
export type { UserAvatarProps } from "./components/UserAvatar";
export { default as UserCombobox } from "./components/UserCombobox";
export { SingleHandleCombobox } from "./components/SingleHandleCombobox";
export type { Actor } from "./types/Actor";
export { AppHeader } from "./components/AppHeader";
export { AppLayout } from "./components/AppLayout";
export { default as ShareDialog } from "./components/ShareDialog";
export { default as ShareDialogV2 } from "./components/ShareDialogV2";
export { default as GroupCombobox } from "./components/GroupCombobox";
export type { GroupView } from "./components/GroupCombobox";
export { resolveHandleToDid, resolveDidToHandle } from "./atprotoDirectory";
export {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuItem,
  SidebarMenuButton,
} from "./components/ui/sidebar";
export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./components/ui/dialog";
export { Button } from "./components/ui/button";
export { Input } from "./components/ui/input";
export {
  // Managers and Sessions
  AuthManager,
  UnauthenticatedError,
} from "./authManager";
export {
  query,
  procedure,
  castRecord,
  listPrivateRecords,
  getPrivateRecord,
  XRPCError,
} from "./habitatClient";
export type { TypedRecord, Fetcher } from "./habitatClient";
export { default as GranteeAvatars } from "./components/GranteeAvatars";

// Utilities
export { default as clientMetadata } from "./clientMetadata";
export { default as reportWebVitals } from "./reportWebVitals";
export {
  searchActorsTypeahead,
  getProfiles,
  getProfile,
} from "./bskyPublicApi";
