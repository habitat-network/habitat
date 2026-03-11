import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { UserAvatar } from "internal";
import { Card, CardFooter, CardTitle } from "internal/components/ui";

export interface CollectionCardProps {
  collection: Omit<CollectionMetadata, "grantees"> & {
    grantees: { did: string; avatar?: string; handle: string }[];
  };
}

export function CollectionCard({ collection }: CollectionCardProps) {
  return (
    <Card key={collection.nsid}>
      <div className="flex items-center justify-between px-6">
        <CardTitle>{collection.nsid}</CardTitle>
        <span className="text-sm text-muted-foreground">
          {collection.recordCount}{" "}
          {collection.recordCount > 1 ? "records" : "record"}
        </span>
      </div>
      <CardFooter>
        <div className="flex items-center justify-between w-full">
          <div className="flex gap-1">
            {collection.grantees.slice(0, 5).map((g) => {
              return <UserAvatar key={g.did} actor={g} size="sm" />;
            })}
          </div>
          <span className="text-sm text-muted-foreground">
            {new Date(collection.lastTouched).toLocaleDateString()}
          </span>
        </div>
      </CardFooter>
    </Card>
  );
}
