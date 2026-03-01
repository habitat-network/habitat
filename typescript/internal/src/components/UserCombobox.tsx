import { AuthManager } from "../authManager";
import { Combobox } from "@base-ui/react";
import { useQuery } from "@tanstack/react-query";
import { useDebounce } from "@uidotdev/usehooks";
import { useState } from "react";

interface Actor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface UserComboboxProps {
  authManager: AuthManager;
}

const UserCombobox = ({ authManager }: UserComboboxProps) => {
  const [value, setValue] = useState<Actor[]>([
    {
      handle: "sashankg.bsky.social",
      displayName: "Sashank Gogula",
    },
  ]);
  const [searchValue, setSearchValue] = useState("");
  const debouncedQuery = useDebounce(searchValue, 250);
  const { data: results = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedQuery],
    queryFn: async () => {
      const params = new URLSearchParams({ q: debouncedQuery, limit: "8" });
      const res = await authManager.fetch(
        `/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
      );
      const data: { actors: Actor[] } = await res.json();
      return data.actors ?? [];
    },
    enabled: !!debouncedQuery.trim(),
  });
  return (
    <Combobox.Root
      items={results}
      filter={null}
      onInputValueChange={setSearchValue}
      itemToStringLabel={(user: Actor) => user.handle}
      multiple
      value={value}
      onValueChange={setValue}
    >
      <Combobox.Chips>
        <Combobox.Value>
          {(value: Actor[]) =>
            value.map((actor) => (
              <Combobox.Chip key={actor.handle}>
                {actor.displayName}
                <Combobox.ChipRemove>×</Combobox.ChipRemove>
              </Combobox.Chip>
            ))
          }
        </Combobox.Value>
      </Combobox.Chips>
      <Combobox.Input />
      <Combobox.Portal>
        <Combobox.Positioner style={{ zIndex: 0 }}>
          <Combobox.Popup
            style={{
              transformOrigin: "var(--transform-origin)",
              maxWidth: "min(var(--available-height), 23rem)",
              maxHeight: "var(--available-height)",
              width: "var(--anchor-width)",
            }}
          >
            <Combobox.List>
              {(actor: Actor) => (
                <Combobox.Item key={actor.handle} value={actor}>
                  {actor.displayName}
                </Combobox.Item>
              )}
            </Combobox.List>
          </Combobox.Popup>
        </Combobox.Positioner>
      </Combobox.Portal>
    </Combobox.Root>
  );
};

export default UserCombobox;
