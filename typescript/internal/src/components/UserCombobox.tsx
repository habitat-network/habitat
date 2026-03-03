import { AuthManager } from "@/authManager";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";
import { useState } from "react";
import { useDebounce } from "@uidotdev/usehooks";
import { useQuery } from "@tanstack/react-query";

interface UserComboboxProps {
  authManager: AuthManager;
}

interface Actor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

const UserCombobox = ({ authManager }: UserComboboxProps) => {
  const [searchValue, setSearchValue] = useState("");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const [value, setValue] = useState<Actor[]>([]);

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedSearchValue],
    queryFn: async () => {
      const params = new URLSearchParams({
        q: debouncedSearchValue,
        limit: "8",
      });
      const res = await authManager.fetch(
        `/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
        "GET",
      );
      const data: { actors: Actor[] } = await res.json();
      return data.actors ?? [];
    },
    enabled: !!debouncedSearchValue.trim(),
  });

  return (
    <Combobox
      items={suggestions}
      onInputValueChange={setSearchValue}
      inputValue={searchValue}
      multiple
      value={value}
      onValueChange={setValue}
    >
      <ComboboxInput />
      <ComboboxContent>
        <ComboboxEmpty>No items found.</ComboboxEmpty>
        <ComboboxList>
          {(item: Actor) => (
            <ComboboxItem key={item.handle} value={item}>
              {item.handle}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
};

export default UserCombobox;
