// Components
export { default as AuthForm } from "./AuthForm";
export { UserAvatar } from "./components/UserAvatar";
export type { UserAvatarProps } from "./components/UserAvatar";
export { default as UserCombobox } from "./components/UserCombobox";
export type { Actor } from "./components/UserCombobox";
export { AppHeader } from "./components/AppHeader";
export { AppLayout } from "./components/AppLayout";
export { default as ShareDialog } from "./components/ShareDialog";
export {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from "./components/ui/sidebar";
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
export type { TypedRecord } from "./habitatClient";

// Utilities
export { default as clientMetadata } from "./clientMetadata";
export { default as reportWebVitals } from "./reportWebVitals";
