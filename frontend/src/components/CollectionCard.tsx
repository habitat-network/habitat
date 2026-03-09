import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { Card, CardTitle, CardDescription, CardFooter, UserAvatar } from "internal";


export interface CollectionCardProps {
    collection: Omit<CollectionMetadata, 'grantees'> & {
        grantees: { did: string, avatar?: string; handle: string }[];
    }
}

export function CollectionCard({ collection }: CollectionCardProps) {
    return (
        <Card key={collection.nsid}>
            <div className="flex items-center justify-between px-6">
                <CardTitle>{collection.nsid}</CardTitle>
                <span className="text-sm text-muted-foreground">{collection.recordCount} {(collection.recordCount > 1) ? "records" : "record"}</span>
            </div>
            <CardDescription className="px-6">Last updated: {new Date(collection.lastTouched).toLocaleDateString()}</CardDescription>
            <CardFooter>
                <div className="flex gap-1">
                    {collection.grantees.map((g) => {
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
            </CardFooter>
        </Card>
    )
}
