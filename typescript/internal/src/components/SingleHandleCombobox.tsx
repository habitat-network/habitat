import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
} from "./ui/combobox";
import { Input } from "./ui/input";
import { useState, useEffect, useRef } from "react";
import { useDebounce } from "@uidotdev/usehooks";
import { useQuery } from "@tanstack/react-query";
import { UserAvatar } from "./UserAvatar";
import { Actor } from "@/types/Actor";
import { searchActorsTypeahead } from "../bskyPublicApi";

interface SingleHandleComboboxProps {
  value: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
}

export const SingleHandleCombobox = ({
  value,
  onValueChange,
  placeholder = "alice.bsky.social",
}: SingleHandleComboboxProps) => {
  const [open, setOpen] = useState(false);
  const [searchValue, setSearchValue] = useState(value || "");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const inputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setSearchValue(value || "");
  }, [value]);

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedSearchValue],
    queryFn: () => searchActorsTypeahead(debouncedSearchValue),
    enabled: !!debouncedSearchValue.trim(),
  });

  return (
    <Combobox
      items={suggestions}
      open={open}
      onOpenChange={setOpen}
      onValueChange={(actor: Actor | null) => {
        if (actor?.handle) {
          onValueChange(actor.handle);
          setSearchValue(actor.handle);
          setOpen(false);
        }
      }}
    >
      <div ref={inputRef}>
        <Input
          placeholder={placeholder}
          value={searchValue}
          onChange={(e) => {
            setSearchValue(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
        />
      </div>
      <ComboboxContent anchor={inputRef}>
        <ComboboxEmpty>No results found.</ComboboxEmpty>
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

export default SingleHandleCombobox;
