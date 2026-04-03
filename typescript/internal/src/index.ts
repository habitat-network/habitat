// Components
export { default as AuthForm } from "./AuthForm";
export { UserAvatar } from "./components/UserAvatar";
export type { UserAvatarProps } from "./components/UserAvatar";
export { default as UserCombobox } from "./components/UserCombobox";
export type { Actor } from "./types/Actor";
export { AppHeader } from "./components/AppHeader";
export { AppLayout } from "./components/AppLayout";
export { default as ShareDialog } from "./components/ShareDialog";
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
export { default as GranteeAvatars } from "./components/GranteeAvatars";

// Utilities
export { default as clientMetadata } from "./clientMetadata";
export { default as reportWebVitals } from "./reportWebVitals";
