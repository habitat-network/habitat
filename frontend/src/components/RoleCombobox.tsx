import { useMemo, useState } from "react";
import type { RoleView } from "@/queries/opensocial";
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  useComboboxAnchor,
} from "internal/components/ui";

// RoleCombobox is a multi-select combobox over a community's declared
// roles, rendering the current selection as chips. Shared by every place
// that lets an admin pick a set of roles: assigning a member's roles, and
// binding roles to a capability or to another role's assignable set.
export function RoleCombobox({
  roles,
  value,
  onValueChange,
  placeholder = "Add a role…",
  disabled,
}: {
  roles: RoleView[];
  value: RoleView[];
  onValueChange: (value: RoleView[]) => void;
  placeholder?: string;
  disabled?: boolean;
}) {
  const [searchValue, setSearchValue] = useState("");
  const anchor = useComboboxAnchor();

  const filtered = useMemo(() => {
    const q = searchValue.trim().toLowerCase();
    return roles.filter((role) => !q || role.name.toLowerCase().includes(q));
  }, [roles, searchValue]);

  return (
    <Combobox
      items={filtered}
      onInputValueChange={setSearchValue}
      inputValue={searchValue}
      multiple
      value={value}
      onValueChange={onValueChange}
      disabled={disabled}
    >
      <ComboboxChips ref={anchor}>
        {value.map((role) => (
          <ComboboxChip key={role.rkey}>{role.name}</ComboboxChip>
        ))}
        <ComboboxChipsInput placeholder={placeholder} />
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>No roles found.</ComboboxEmpty>
        <ComboboxList>
          {(role: RoleView) => (
            <ComboboxItem key={role.rkey} value={role}>
              {role.name}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
