import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

/** Bluesky searchActorsTypeahead API response actor (ProfileViewBasic). */
export interface ActorProfile {
  did: string;
  handle: string;
  displayName?: string;
  avatar?: string;
}

const BLUESKY_API = "https://public.api.bsky.app";
const DEBOUNCE_MS = 300;
const MIN_QUERY_LENGTH = 2;

interface ActorTypeaheadProps {
  /** Currently selected DIDs. */
  value: string[];
  /** Called when selection changes. */
  onChange: (dids: string[]) => void;
  /** Placeholder for the search input. */
  placeholder?: string;
  /** Label for the field. */
  label?: string;
  disabled?: boolean;
}

async function searchActors(q: string): Promise<ActorProfile[]> {
  const url = new URL(
    "/xrpc/app.bsky.actor.searchActorsTypeahead",
    BLUESKY_API,
  );
  url.searchParams.set("q", q);
  url.searchParams.set("limit", "10");

  const res = await fetch(url.toString());
  if (!res.ok) throw new Error(`Search failed: ${res.status}`);

  const data = (await res.json()) as { actors?: ActorProfile[] };
  return data.actors ?? [];
}

export function ActorTypeahead({
  value,
  onChange,
  placeholder = "Search by handle or name...",
  label = "Invite",
  disabled = false,
}: ActorTypeaheadProps) {
  const [inputValue, setInputValue] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [isOpen, setIsOpen] = useState(false);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const selectedActorInfoRef = useRef<Map<string, ActorProfile>>(new Map());

  useEffect(() => {
    if (inputValue.length < MIN_QUERY_LENGTH) {
      setDebouncedQuery("");
      return;
    }
    const id = setTimeout(() => setDebouncedQuery(inputValue), DEBOUNCE_MS);
    return () => clearTimeout(id);
  }, [inputValue]);

  const { data: actors = [], isFetching } = useQuery({
    queryKey: ["actorSearch", debouncedQuery],
    queryFn: () => searchActors(debouncedQuery),
    enabled: debouncedQuery.length >= MIN_QUERY_LENGTH,
    staleTime: 60 * 1000,
  });

  const selectedSet = new Set(value);
  const availableActors = actors.filter((a) => !selectedSet.has(a.did));

  const addActor = useCallback(
    (actor: ActorProfile) => {
      if (value.includes(actor.did)) return;
      selectedActorInfoRef.current.set(actor.did, actor);
      onChange([...value, actor.did]);
      setInputValue("");
      setDebouncedQuery("");
      setHighlightIndex(0);
    },
    [value, onChange],
  );

  const removeActor = useCallback(
    (did: string) => {
      selectedActorInfoRef.current.delete(did);
      onChange(value.filter((d) => d !== did));
    },
    [value, onChange],
  );

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setIsOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  function handleInputFocus() {
    if (
      availableActors.length > 0 ||
      debouncedQuery.length >= MIN_QUERY_LENGTH
    ) {
      setIsOpen(true);
    }
  }

  function handleInputChange(e: React.ChangeEvent<HTMLInputElement>) {
    setInputValue(e.target.value);
    setIsOpen(true);
    setHighlightIndex(0);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!isOpen || availableActors.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlightIndex((i) => (i < availableActors.length - 1 ? i + 1 : 0));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlightIndex((i) => (i > 0 ? i - 1 : availableActors.length - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      addActor(availableActors[highlightIndex]);
    } else if (e.key === "Escape") {
      setIsOpen(false);
    }
  }

  function getActorDisplay(did: string): ActorProfile | undefined {
    return (
      selectedActorInfoRef.current.get(did) ?? actors.find((a) => a.did === did)
    );
  }

  return (
    <div ref={containerRef} style={{ position: "relative" }}>
      <label>
        {label}:
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: "0.25rem",
            alignItems: "center",
            padding: "0.5rem",
            border: "1px solid var(--pico-muted-border-color)",
            borderRadius: "var(--pico-border-radius)",
            minHeight: "2.5rem",
            backgroundColor: "var(--pico-background-color)",
          }}
        >
          {value.map((did) => {
            const cached = getActorDisplay(did);
            const labelText =
              cached?.displayName ??
              (cached?.handle ? `@${cached.handle}` : null) ??
              did;
            return (
              <span
                key={did}
                role="button"
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: "0.25rem",
                  padding: "0.125rem 0.5rem",
                  backgroundColor: "var(--pico-muted-border-color)",
                  borderRadius: "var(--pico-border-radius)",
                  fontSize: "0.875rem",
                }}
              >
                {cached?.avatar && (
                  <img
                    src={cached.avatar}
                    alt=""
                    width={16}
                    height={16}
                    style={{ borderRadius: "50%", flexShrink: 0 }}
                  />
                )}
                <span
                  style={{ flexShrink: 0, color: "var(--pico-muted-color)" }}
                >
                  {labelText}
                </span>
                <button
                  type="button"
                  aria-label={`Remove ${labelText}`}
                  onClick={() => removeActor(did)}
                  disabled={disabled}
                  style={{
                    marginLeft: "0.25rem",
                    padding: 0,
                    background: "none",
                    border: "none",
                    cursor: disabled ? "default" : "pointer",
                    fontSize: "1rem",
                    lineHeight: 1,
                  }}
                >
                  ×
                </button>
              </span>
            );
          })}
          <input
            type="text"
            value={inputValue}
            onChange={handleInputChange}
            onFocus={handleInputFocus}
            onKeyDown={handleKeyDown}
            placeholder={value.length === 0 ? placeholder : "Add more..."}
            disabled={disabled}
            autoComplete="off"
            style={{
              flex: 1,
              minWidth: "8rem",
              border: "none",
              outline: "none",
              background: "transparent",
            }}
          />
        </div>
      </label>

      {isOpen && (
        <ul
          role="listbox"
          style={{
            position: "absolute",
            top: "100%",
            left: 0,
            right: 0,
            margin: 0,
            marginTop: "0.25rem",
            padding: 0,
            listStyle: "none",
            backgroundColor: "var(--pico-background-color)",
            border: "1px solid var(--pico-muted-border-color)",
            borderRadius: "var(--pico-border-radius)",
            maxHeight: "12rem",
            overflowY: "auto",
            zIndex: 100,
          }}
        >
          {isFetching && debouncedQuery.length >= MIN_QUERY_LENGTH && (
            <li style={{ padding: "0.75rem" }}>Searching...</li>
          )}
          {!isFetching &&
            debouncedQuery.length >= MIN_QUERY_LENGTH &&
            availableActors.length === 0 && (
              <li
                style={{ padding: "0.75rem", color: "var(--pico-muted-color)" }}
              >
                No users found
              </li>
            )}
          {!isFetching &&
            availableActors.map((actor, i) => (
              <li
                key={actor.did}
                role="option"
                aria-selected={i === highlightIndex}
                onClick={() => addActor(actor)}
                onMouseEnter={() => setHighlightIndex(i)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: "0.5rem",
                  padding: "0.5rem 0.75rem",
                  cursor: "pointer",
                  backgroundColor:
                    i === highlightIndex
                      ? "rgba(0, 0, 0, 0.08)"
                      : "transparent",
                }}
              >
                {actor.avatar ? (
                  <img
                    src={actor.avatar}
                    alt=""
                    width={32}
                    height={32}
                    style={{ borderRadius: "50%" }}
                  />
                ) : (
                  <div
                    style={{
                      width: 32,
                      height: 32,
                      borderRadius: "50%",
                      backgroundColor: "var(--pico-muted-border-color)",
                    }}
                  />
                )}
                <div style={{ flex: 1, minWidth: 0 }}>
                  {actor.displayName ? (
                    <>
                      <div style={{ fontWeight: 500 }}>{actor.displayName}</div>
                      <div
                        style={{
                          fontSize: "0.75rem",
                          color: "var(--pico-muted-color)",
                        }}
                      >
                        @{actor.handle}
                      </div>
                    </>
                  ) : (
                    <div style={{ fontWeight: 500 }}>
                      {actor.handle ? `@${actor.handle}` : actor.did}
                    </div>
                  )}
                </div>
              </li>
            ))}
        </ul>
      )}
    </div>
  );
}
