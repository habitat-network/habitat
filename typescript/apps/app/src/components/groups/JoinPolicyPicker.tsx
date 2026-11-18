import { RadioGroup, RadioGroupItem } from "internal/components/ui";

export type JoinPolicy = "open" | "invite" | "request";

const POLICIES: { value: JoinPolicy; label: string; description: string }[] = [
  {
    value: "invite",
    label: "Invite only",
    description: "People join when a member invites them.",
  },
  {
    value: "open",
    label: "Open",
    description: "Anyone who finds the group can join.",
  },
  {
    value: "request",
    label: "By request",
    description: "People ask to join and an admin approves.",
  },
];

// JoinPolicyPicker chooses who can join a group (the profile's joinPolicy).
export function JoinPolicyPicker({
  value,
  onChange,
  disabled,
}: {
  value: JoinPolicy;
  onChange: (v: JoinPolicy) => void;
  disabled?: boolean;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as JoinPolicy)}
      disabled={disabled}
      className="grid gap-2"
    >
      {POLICIES.map((p) => (
        <label
          key={p.value}
          className="flex cursor-pointer items-start gap-3 rounded-xl border p-3 has-data-checked:border-primary has-data-checked:bg-primary/5"
        >
          <RadioGroupItem value={p.value} className="mt-0.5" />
          <span>
            <span className="block text-sm font-medium">{p.label}</span>
            <span className="block text-xs text-muted-foreground">
              {p.description}
            </span>
          </span>
        </label>
      ))}
    </RadioGroup>
  );
}
