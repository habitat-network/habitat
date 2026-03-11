import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { Card, CardTitle, CardFooter, UserAvatar } from "internal";

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
              return (
                <UserAvatar
                  key={g.did}
                  src={g.avatar}
                  handle={g.handle}
                  size="sm"
                />
              );
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
