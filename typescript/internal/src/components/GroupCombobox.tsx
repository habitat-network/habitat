import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "./ui/combobox";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { NetworkHabitatGroupsDefs } from "api";
import { AuthManager } from "../authManager";
import { query } from "../habitatClient";

export type GroupView = NetworkHabitatGroupsDefs.GroupView;

// homeProxyHeader targets the home server (which implements groups.*) via pear
// service proxying. Hardcoded to the local-dev home domain for now.
function homeProxyHeader(): Headers {
  return new Headers({
    "Atproto-Proxy": "did:web:home.local.habitat.network#groups",
  });
}

interface GroupComboboxProps {
  authManager: AuthManager;
  value: GroupView | null;
  onValueChange: (value: GroupView | null) => void;
  placeholder?: string;
}

// GroupCombobox lets a user pick a single group from the list of groups they can
// see. Selecting a group emits the full GroupView so callers have its URI and
// display name.
export const GroupCombobox = ({
  authManager,
  value,
  onValueChange,
  placeholder = "Search groups…",
}: GroupComboboxProps) => {
  const [searchValue, setSearchValue] = useState(value?.name ?? "");

  // Clear the input text when the caller resets the selection (e.g. after the
  // selected group is added).
  useEffect(() => {
    if (value === null) setSearchValue("");
  }, [value]);

  const { data: groups = [] } = useQuery<GroupView[]>({
    queryKey: ["groups", "listGroups"],
    queryFn: async () => {
      const { groups } = await query(
        "network.habitat.groups.listGroups",
        {},
        { authManager, headers: homeProxyHeader() },
      );
      return groups;
    },
  });

  const filtered = useMemo(() => {
    const q = searchValue.trim().toLowerCase();
    if (!q) return groups;
    return groups.filter((g) => g.name.toLowerCase().includes(q));
  }, [groups, searchValue]);

  return (
    <Combobox
      items={filtered}
      onInputValueChange={setSearchValue}
      onValueChange={(group: GroupView | null) => {
        onValueChange(group);
      }}
    >
      <ComboboxInput placeholder={placeholder} value={searchValue} />
      <ComboboxContent>
        <ComboboxEmpty>No groups found.</ComboboxEmpty>
        <ComboboxList>
          {(item: GroupView) => (
            <ComboboxItem key={item.uri} value={item}>
              {item.name}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
};

export default GroupCombobox;
