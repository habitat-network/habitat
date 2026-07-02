import { createFileRoute, Link } from "@tanstack/react-router";
import {
  collectionsListQueryOptions,
  type CollectionView,
} from "@/queries/collections";
import { Card, CardContent } from "internal/components/ui";

export const Route = createFileRoute("/_requireAuth/collections/")({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      collectionsListQueryOptions(context.authManager),
    ),
  pendingComponent: () => <p className="py-8">Loading collections…</p>,
  component: CollectionsGrid,
});

function CollectionsGrid() {
  const collections = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-6 py-6">
      <div>
        <h1 className="text-2xl font-semibold">Collections</h1>
        <p className="text-muted-foreground text-sm">
          Browse the org’s data by collection type. Only records you can see are
          counted.
        </p>
      </div>

      {collections.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            There are no records you can see yet.
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {collections.map((collection) => (
            <CollectionCard
              key={collection.collection}
              collection={collection}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function CollectionCard({ collection }: { collection: CollectionView }) {
  const parts = collection.collection.split(".");
  const leaf = parts[parts.length - 1];
  const prefix = parts.slice(0, -1).join(".");

  return (
    <Card className="hover:border-primary transition-colors">
      <Link
        to="/collections/$collection"
        params={{ collection: collection.collection }}
      >
        <CardContent className="flex flex-col gap-2 py-5">
          <span className="break-all">
            {prefix && (
              <span className="text-muted-foreground">{prefix}.</span>
            )}
            <span className="font-medium">{leaf}</span>
          </span>
          <span className="text-sm text-muted-foreground">
            {collection.recordCount}{" "}
            {collection.recordCount === 1 ? "record" : "records"}
          </span>
        </CardContent>
      </Link>
    </Card>
  );
}
