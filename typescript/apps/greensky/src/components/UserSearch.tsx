import { useQuery } from "@tanstack/react-query";
import { useState, useEffect, useRef } from "react";
import { AuthManager } from "internal/authManager.js";

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
        style={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: "4px",
          border: "1px solid var(--pico-form-element-border-color)",
          borderRadius: "var(--pico-border-radius)",
          padding: "4px 8px",
          minHeight: "42px",
          backgroundColor: "var(--pico-form-element-background-color)",
          cursor: "text",
        }}
        onClick={() => inputRef.current?.focus()}
      >
        {specificUsers.map((u) => (
          <kbd
            key={u}
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "4px",
              whiteSpace: "nowrap",
            }}
          >
            @{u}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onRemoveUser(u);
              }}
              aria-label={`Remove ${u}`}
              style={{
                all: "unset",
                cursor: "pointer",
                fontSize: "0.75rem",
                lineHeight: 1,
                opacity: 0.5,
                padding: "0 2px",
              }}
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
          style={{
            all: "unset",
            flex: 1,
            minWidth: "120px",
            cursor: "text",
          }}
        />
      </div>
      {visibleSuggestions.length > 0 && (
        <ul
          role="list"
          style={{
            margin: 0,
            padding: "4px 0",
            listStyle: "none",
            background: "var(--pico-card-background-color)",
            border: "1px solid var(--pico-form-element-border-color)",
            borderRadius: "var(--pico-border-radius)",
            boxShadow: "0 4px 12px rgba(0,0,0,0.12)",
          }}
        >
          {visibleSuggestions.map((actor) => (
            <li
              key={actor.handle}
              onClick={() => addUser(actor.handle)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: "8px",
                padding: "6px 12px",
                cursor: "pointer",
                margin: 0,
              }}
            >
              {actor.avatar && (
                <img
                  src={actor.avatar}
                  width={24}
                  height={24}
                  style={{ borderRadius: "50%", flexShrink: 0 }}
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
