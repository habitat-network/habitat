import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { getProfiles, HabitatLogo, UserAvatar, type Actor } from "internal";
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "internal/components/ui";
import { PageHeader } from "@/components/PageHeader";
import { listDocs } from "@/server/functions";

function ownerLabel(owner: Actor | undefined, ownerDid: string): string {
  return owner?.displayName || owner?.handle || ownerDid;
}

function OwnerCell({
  owner,
  ownerDid,
}: {
  owner: Actor | undefined;
  ownerDid: string;
}) {
  return (
    <div className="flex items-center gap-2">
      <UserAvatar size="sm" actor={owner ?? { did: ownerDid }} />
      <span>{ownerLabel(owner, ownerDid)}</span>
    </div>
  );
}

export const Route = createFileRoute("/_requireAuth/")({
  component() {
    const { data: docs = [] } = useQuery({
      queryKey: ["docs"],
      queryFn: () => listDocs(),
    });

    const ownerDids = [...new Set(docs.map((doc) => doc.ownerDid))];
    const { data: owners = [] } = useQuery({
      queryKey: ["profiles", ownerDids],
      queryFn: () => getProfiles(ownerDids),
      enabled: ownerDids.length > 0,
    });
    const ownersByDid = new Map(owners.map((owner) => [owner.did, owner]));

    return (
      <div className="flex flex-col h-full">
        <PageHeader />
        <div className="flex-1 overflow-auto p-4">
          <div className="px-2">
            <h1 className="text-2xl font-semibold mb-1">Chalk</h1>
            <p className="flex items-center gap-1 text-sm text-muted-foreground mb-4">
              by{" "}
              <a
                href="https://habitat.network"
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 hover:underline"
              >
                <HabitatLogo size={16} /> Habitat
              </a>
            </p>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Document</TableHead>
                <TableHead>Owner</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {docs.map((doc) => (
                <TableRow key={doc.docId}>
                  <TableCell>
                    <Link
                      to="/$uri"
                      params={{ uri: doc.docId }}
                      className="hover:underline"
                    >
                      {doc.title}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <OwnerCell
                      owner={ownersByDid.get(doc.ownerDid)}
                      ownerDid={doc.ownerDid}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>
    );
  },
});
