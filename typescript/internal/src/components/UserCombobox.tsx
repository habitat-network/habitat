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
import { resolveHandleToDid } from "../atprotoDirectory";

// eslint-disable-next-line no-console
console.log("[UserCombobox debug] module loaded");

interface UserComboboxProps {
  value?: Actor[];
  onValueChange: (value: Actor[]) => void;
}

const UserCombobox = ({ value, onValueChange }: UserComboboxProps) => {
  const [searchValue, setSearchValue] = useState("");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const trimmedSearchValue = debouncedSearchValue.trim();
  const anchor = useComboboxAnchor();

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", trimmedSearchValue],
    queryFn: () => searchActorsTypeahead(trimmedSearchValue),
    enabled: !!trimmedSearchValue,
  });

  // The public typeahead above only indexes handles known to Bluesky's
  // directory, so it never surfaces org (habitat-hosted did:web) handles.
  // As a stopgap until org handles get a proper permissioned typeahead,
  // resolve whatever's typed as a raw handle and offer it as an extra,
  // selectable item — this covers org handles as well as any other valid
  // atproto handle the public search just hasn't indexed.
  const alreadySuggested = suggestions.some(
    (actor) => actor.handle?.toLowerCase() === trimmedSearchValue.toLowerCase(),
  );
  const resolveEnabled =
    !!trimmedSearchValue &&
    !alreadySuggested &&
    trimmedSearchValue.includes(".");
  // eslint-disable-next-line no-console
  console.log("[UserCombobox debug] resolve query enabled?", {
    trimmedSearchValue,
    alreadySuggested,
    resolveEnabled,
  });
  const { data: resolvedActor } = useQuery<Actor | null>({
    queryKey: ["actorResolve", trimmedSearchValue],
    queryFn: async () => {
      // eslint-disable-next-line no-console
      console.log(
        "[UserCombobox debug] calling resolveHandleToDid for",
        trimmedSearchValue,
      );
      try {
        const did = await resolveHandleToDid(trimmedSearchValue);
        // eslint-disable-next-line no-console
        console.log("[UserCombobox debug] resolved did", did);
        return { did, handle: trimmedSearchValue };
      } catch (err) {
        // eslint-disable-next-line no-console
        console.log("[UserCombobox debug] resolveHandleToDid threw", err);
        return null;
      }
    },
    enabled: resolveEnabled,
  });

  const items = resolvedActor ? [...suggestions, resolvedActor] : suggestions;

  return (
    <Combobox
      items={items}
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
