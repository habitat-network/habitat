import { useQuery } from "@tanstack/react-query";
import { useState, useEffect, useRef } from "react";
import { AuthManager } from "internal";

function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debouncedValue;
}

interface Actor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface UserSearchProps {
  authManager: AuthManager;
  specificUsers: string[];
  onAddUser: (handle: string) => void;
  onRemoveUser: (handle: string) => void;
}

export function UserSearch({
  authManager,
  specificUsers,
  onAddUser,
  onRemoveUser,
}: UserSearchProps) {
  const [userQuery, setUserQuery] = useState("");
  const debouncedQuery = useDebounce(userQuery, 250);
  const inputRef = useRef<HTMLInputElement>(null);

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedQuery],
    queryFn: async () => {
      const params = new URLSearchParams({ q: debouncedQuery, limit: "8" });
      const res = await authManager.fetch(
        `/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
        "GET",
        null,
        new Headers(),
      );
      const data: { actors: Actor[] } = await res.json();
      return data.actors ?? [];
    },
    enabled: !!debouncedQuery.trim(),
  });

  const visibleSuggestions = debouncedQuery.trim() ? suggestions : [];

  const addUser = (handle: string) => {
    onAddUser(handle.trim().replace(/^@/, ""));
    setUserQuery("");
    inputRef.current?.focus();
  };

  return (
    <div>
      <div
        role="group"
        className="flex flex-wrap items-center gap-1 border border-input rounded-lg p-2 min-h-[42px] bg-background cursor-text"
        onClick={() => inputRef.current?.focus()}
      >
        {specificUsers.map((u) => (
          <kbd
            key={u}
            className="inline-flex items-center gap-1 whitespace-nowrap"
          >
            @{u}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onRemoveUser(u);
              }}
              aria-label={`Remove ${u}`}
              className="cursor-pointer text-xs leading-none opacity-50 px-0.5"
              style={{ all: "unset" }}
            >
              ✕
            </button>
          </kbd>
        ))}
        <input
          ref={inputRef}
          type="text"
          placeholder={specificUsers.length === 0 ? "Search by handle…" : ""}
          value={userQuery}
          onChange={(e) => setUserQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              if (visibleSuggestions.length > 0)
                addUser(visibleSuggestions[0].handle);
              else if (userQuery.trim()) addUser(userQuery);
            }
            if (
              e.key === "Backspace" &&
              !userQuery &&
              specificUsers.length > 0
            ) {
              onRemoveUser(specificUsers[specificUsers.length - 1]);
            }
          }}
          className="flex-1 min-w-[120px] cursor-text"
          style={{ all: "unset" }}
        />
      </div>
      {visibleSuggestions.length > 0 && (
        <ul
          role="list"
          className="m-0 py-1 list-none bg-card border border-input rounded-lg shadow-md"
        >
          {visibleSuggestions.map((actor) => (
            <li
              key={actor.handle}
              onClick={() => addUser(actor.handle)}
              className="flex items-center gap-2 px-3 py-1.5 cursor-pointer hover:bg-accent m-0"
            >
              {actor.avatar && (
                <img
                  src={actor.avatar}
                  width={24}
                  height={24}
                  className="rounded-full flex-shrink-0"
                />
              )}
              <span>
                {actor.displayName && <strong>{actor.displayName}</strong>} @
                {actor.handle}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
