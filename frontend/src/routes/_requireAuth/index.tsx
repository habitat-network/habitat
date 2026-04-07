import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { query, searchRecords } from "internal";
import {
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Item,
  ItemGroup,
  ItemHeader,
  ItemTitle,
} from "internal/components/ui";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { CollectionCard } from "@/components/CollectionCard";
import { App } from "api/types/network/habitat/listConnectedApps";
import { Record as SearchRecord } from "api/types/network/habitat/repo/searchRecords";

import { Search } from "lucide-react";
import { useState, useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const { authManager } = context;
    const appData = await query(
      "network.habitat.listConnectedApps",
      {},
      { authManager },
    );

    // List collections for manage your data preview
    const data = await query(
      "network.habitat.repo.listCollections",
      {},
      { authManager },
    );
    const collections = data.collections.slice(0, 3); // Just show the first three in the preview

    const apps = appData.apps.filter(
      (app) => app.clientUri !== `https://${__DOMAIN__}`,
    );
    return { collections, apps: apps };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    return <AuthenticatedHome />;
  },
});

function useDebounce<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return debounced;
}

function parseUri(uri: string) {
  // habitat://<did>/<collection>/<rkey>
  const parts = uri.split("/");
  return { did: parts[2], collection: parts[3], rkey: parts[4] };
}

interface RecentlyUsedProps {
  apps: App[];
}

function RecentlyUsed({ apps }: RecentlyUsedProps) {
  return (
    <Card size="sm" className="flex-1 min-w-128">
      <CardHeader>
        <CardTitle>Recently used</CardTitle>
      </CardHeader>
      <CardContent>
        <ItemGroup className="grid grid-cols-3">
          {apps.map((app) => (
            <Item
              key={app.clientID}
              render={<Link to={app.clientUri} />}
              variant="muted"
            >
              <ItemHeader className="rounded bg-background p-2">
                {app.logoUri ? (
                  <img
                    src={app.logoUri}
                    alt={app.name}
                    className="w-12 h-12 object-contain mx-auto"
                  />
                ) : null}
              </ItemHeader>
              <ItemTitle className="text-xs text-center truncate w-full px-1">
                {app.name}
              </ItemTitle>
            </Item>
          ))}
        </ItemGroup>
      </CardContent>
    </Card>
  );
}

interface ManageDataPreviewProps {
  collections: CollectionMetadata[];
}

function ManageDataPreview({ collections }: ManageDataPreviewProps) {
  const { authManager } = Route.useRouteContext();
  return (
    <Card size="sm" className="flex-1 min-w-128">
      <CardHeader>
        <CardTitle>Manage your data</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid">
          {collections.map((collection) => {
            return (
              <CollectionCard
                key={collection.nsid}
                authManager={authManager}
                collection={collection}
              />
            );
          })}
        </div>
      </CardContent>
      <CardFooter>
        <Button
          variant="ghost"
          className="w-full"
          render={<Link to="/collections" className="text-sm !no-underline" />}
        >
          See all →
        </Button>
      </CardFooter>
    </Card>
  );
}

function AuthenticatedHome() {
  const { collections, apps } = Route.useLoaderData()!;
  const { authManager } = Route.useRouteContext();
  const navigate = useNavigate();

  const [inputValue, setInputValue] = useState("");
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const debouncedQuery = useDebounce(inputValue, 300);

  const { data, isLoading } = useQuery({
    queryKey: ["searchRecords", debouncedQuery],
    queryFn: () => searchRecords(authManager, debouncedQuery),
    enabled: debouncedQuery.trim().length > 0,
    staleTime: 30_000,
  });

  const results: SearchRecord[] = data?.records ?? [];

  // Open dropdown whenever we have a query
  useEffect(() => {
    setIsOpen(debouncedQuery.trim().length > 0);
  }, [debouncedQuery]);

  // Close when clicking outside
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

  function handleSelect(record: SearchRecord) {
    const { collection } = parseUri(record.uri);
    setIsOpen(false);
    setInputValue("");
    navigate({ to: "/collections/$collection", params: { collection } });
  }

  return (
    <>
      <div className="flex-1 flex flex-col gap-4 justify-center min-h-[60vh]">
        <h1 className="text-2xl">Welcome to Habitat!</h1>
        <div ref={containerRef} className="relative">
          <InputGroup>
            <InputGroupInput
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              onFocus={() => results.length > 0 && setIsOpen(true)}
              placeholder="Search your data for anything..."
            />
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
          </InputGroup>

          {isOpen && (
            <div className="absolute top-full left-0 right-0 z-50 mt-1 rounded-xl border border-border bg-popover shadow-lg overflow-hidden">
              {isLoading ? (
                <div className="px-4 py-3 text-sm text-muted-foreground">
                  Searching…
                </div>
              ) : results.length === 0 ? (
                <div className="px-4 py-3 text-sm text-muted-foreground">
                  No results found.
                </div>
              ) : (
                results.map((record) => {
                  const { collection, rkey } = parseUri(record.uri);
                  return (
                    <button
                      key={record.uri}
                      className="w-full flex flex-col gap-0.5 px-4 py-3 text-left hover:bg-muted transition-colors border-b border-border last:border-0"
                      onClick={() => handleSelect(record)}
                    >
                      <span className="text-xs text-muted-foreground font-medium">
                        {collection}
                      </span>
                      <span className="text-sm truncate">{rkey}</span>
                    </button>
                  );
                })
              )}
            </div>
          )}
        </div>
      </div>

      <div className="flex gap-4 flex-wrap">
        <RecentlyUsed apps={apps} />
        <ManageDataPreview collections={collections} />
      </div>
    </>
  );
}
