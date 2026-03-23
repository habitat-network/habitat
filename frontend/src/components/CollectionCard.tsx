import { Link } from "@tanstack/react-router";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { AuthManager, GranteeAvatars } from "internal";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemTitle,
} from "internal/components/ui";

export interface CollectionCardProps {
  authManager: AuthManager;
  collection: CollectionMetadata;
}

export function CollectionCard({
  collection,
  authManager,
}: CollectionCardProps) {
  const nsidParts = collection.nsid.split(".");
  return (
    <Item
      variant="muted"
      render={
        <Link
          to="/collections/$collection"
          params={{ collection: collection.nsid }}
        />
      }
    >
      <ItemContent>
        <ItemTitle>
          <span>
            {nsidParts.map((p, i) => {
              if (i === nsidParts.length - 1) {
                return (
                  <span className="text-base" key={i}>
                    {p}
                  </span>
                );
              } else {
                return (
                  <span key={i} className="text-muted-foreground">
                    {p}.
                  </span>
                );
              }
            })}
          </span>
        </ItemTitle>
        <div className="flex gap-1">
          <ItemDescription>
            {collection.recordCount}{" "}
            {collection.recordCount > 1 ? "records" : "record"}
          </ItemDescription>
          <ItemDescription>•</ItemDescription>
          <ItemDescription>
            {new Date(collection.lastTouched).toLocaleDateString()}
          </ItemDescription>
        </div>
      </ItemContent>
      <ItemActions>
        <GranteeAvatars
          authManager={authManager}
          grantees={collection.grantees}
          uri={collection.nsid}
          max={3}
          size="sm"
        />
      </ItemActions>
    </Item>
  );
}
