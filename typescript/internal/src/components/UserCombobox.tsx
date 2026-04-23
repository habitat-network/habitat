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
} from "./ui/combobox";
import { useState } from "react";
import { useDebounce } from "@uidotdev/usehooks";
import { useQuery } from "@tanstack/react-query";
import { UserAvatar } from "./UserAvatar";
import { Actor } from "@/types/Actor";
import { searchActorsTypeahead } from "../bskyPublicApi";

interface UserComboboxProps {
  value?: Actor[];
  onValueChange: (value: Actor[]) => void;
}

const UserCombobox = ({ value, onValueChange }: UserComboboxProps) => {
  const [searchValue, setSearchValue] = useState("");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const anchor = useComboboxAnchor();

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedSearchValue],
    queryFn: () => searchActorsTypeahead(debouncedSearchValue),
    enabled: !!debouncedSearchValue.trim(),
  });

  return (
    <Combobox
      items={suggestions}
      onInputValueChange={setSearchValue}
      inputValue={searchValue}
      multiple
      value={value}
      onValueChange={onValueChange}
    >
      <ComboboxChips ref={anchor}>
        {value?.map((actor) => (
          <ComboboxChip key={actor.handle}>
            {actor.avatar && (
              <img
                src={actor.avatar}
                width={16}
                height={16}
                className="rounded-full flex-shrink-0"
                alt=""
              />
            )}
            @{actor.handle}
          </ComboboxChip>
        ))}
        <ComboboxChipsInput placeholder="Search by handle…" />
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>No items found.</ComboboxEmpty>
        <ComboboxList>
          {(item: Actor) => (
            <ComboboxItem key={item.handle} value={item}>
              <UserAvatar actor={item} size="sm" />
              {item.displayName || item.handle}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
};

export default UserCombobox;
