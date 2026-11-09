// Components
export { default as AuthForm } from "./AuthForm";
export {
  default as SignInForm,
  EMAIL_DOMAIN_NOT_FOUND_MESSAGE,
} from "./SignInForm";
export { UserAvatar } from "./components/UserAvatar";
export type { UserAvatarProps } from "./components/UserAvatar";
export { UserDisplayName } from "./components/UserDisplayName";
export type { UserDisplayNameProps } from "./components/UserDisplayName";
export { OrgAvatar } from "./components/OrgAvatar";
export type { OrgAvatarProps } from "./components/OrgAvatar";
export { default as UserCombobox } from "./components/UserCombobox";
export { SingleHandleCombobox } from "./components/SingleHandleCombobox";
export type { Actor } from "./types/Actor";
export { AppHeader } from "./components/AppHeader";
export { AppLayout } from "./components/AppLayout";
export { HabitatLogo } from "./components/HabitatLogo";
export { default as ShareDialog } from "./components/ShareDialog";
export type {
  Role as ShareDialogRole,
  Grantee as ShareDialogGrantee,
} from "./components/ShareDialog";
export {
  resolveHandleToDid,
  resolveDidToHandle,
  resolveSpaceHost,
  resolveDidService,
} from "./atprotoDirectory";
export {
  createSpaceSignatureHeaders,
  spaceCredentialKid,
  wantCommitMacHeaders,
  wantRecordMacHeaders,
} from "./spaceSignature";
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
// Managers and Sessions. Type-only: AuthManager's browser OAuth client opens a
// BroadcastChannel as soon as it loads, which breaks non-browser runtimes
// (e.g. chalk's worker) that import this entry point. Construct one via the
// "internal/auth" entry point instead.
export type { AuthManager } from "./authManager";
export {
  LOGIN_HINT_COOKIE_NAME,
  readRememberedLogin,
  forgetRememberedLogin,
} from "./rememberedLogin";
export type { RememberedLogin } from "./rememberedLogin";

// Utilities
export { default as clientMetadata } from "./clientMetadata";
export { default as reportWebVitals } from "./reportWebVitals";
export {
  searchActorsTypeahead,
  getProfiles,
  getProfile,
} from "./bskyPublicApi";
