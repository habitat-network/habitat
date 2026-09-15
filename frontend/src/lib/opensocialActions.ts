// Mirrors the `action` knownValues in lexicons/community/opensocial/permissions.json
// and the internal/opensocial.Action constants (Go). Kept in sync manually
// since actions are a small, stable enumeration for the standard.
export const OPENSOCIAL_ACTIONS: {
  action: string;
  label: string;
  hint: string;
}[] = [
  {
    action: "community.configure",
    label: "Configure community",
    hint: "Edit the profile, rules, roles, and permissions.",
  },
  {
    action: "space.create",
    label: "Create spaces",
    hint: "Create a modality space under the community DID.",
  },
  {
    action: "space.configure",
    label: "Configure spaces",
    hint: "Change a space's config, including who can read it.",
  },
  { action: "space.delete", label: "Delete spaces", hint: "Remove a space." },
  {
    action: "role.assign",
    label: "Assign roles",
    hint: "Grant and revoke roles, bounded by the assignable roles below.",
  },
  {
    action: "eject",
    label: "Eject members",
    hint: "Remove a member, bounded by the assignable roles below.",
  },
  {
    action: "invite",
    label: "Invite",
    hint: "Issue invites to join the community.",
  },
  { action: "admit", label: "Admit", hint: "Approve join requests." },
  {
    action: "mod.read",
    label: "View moderation queue",
    hint: "See the moderation queue and subject histories.",
  },
  {
    action: "mod.resolve",
    label: "Resolve reports",
    hint: "Resolve or escalate a subject, add notes.",
  },
  {
    action: "label",
    label: "Apply labels",
    hint: "Apply and negate labels (excluding hide/takedown).",
  },
  {
    action: "takedown",
    label: "Takedown",
    hint: "Apply and negate hide/takedown labels.",
  },
];
