import { Link } from "@tanstack/react-router";
import { SpaceRef } from "@atproto/syntax";
import {
  Card,
  CardContent,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import type { SpaceView } from "@/queries/spaces";
import { DidHoverCard } from "@/components/DidHoverCard";

export interface SpacesTableProps {
  spaces: SpaceView[];
  // Columns whose value is constant across the whole listing are dropped —
  // the calling page already states it in its heading and breadcrumb.
  showOwner?: boolean;
  showType?: boolean;
  emptyMessage?: string;
}

export function SpacesTable({
  spaces,
  showOwner = false,
  showType = false,
  emptyMessage = "No spaces here.",
}: SpacesTableProps) {
  if (spaces.length === 0) {
    return (
      <Card>
        <CardContent className="py-10 text-center text-muted-foreground">
          {emptyMessage}
        </CardContent>
      </Card>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Space key</TableHead>
          {showType && <TableHead>Type</TableHead>}
          {showOwner && <TableHead>Owner</TableHead>}
        </TableRow>
      </TableHeader>
      <TableBody>
        {spaces.map((space) => {
          const ref = SpaceRef.parse(space.uri);
          const params = {
            spaceOwner: ref.spaceDid,
            spaceType: ref.spaceType,
            spaceKey: ref.skey,
          };
          return (
            <TableRow key={space.uri}>
              <TableCell className="font-mono">
                <Link
                  to="/spaces/$spaceOwner/$spaceType/$spaceKey"
                  params={params}
                  className="hover:underline"
                >
                  {ref.skey}
                </Link>
              </TableCell>
              {showType && (
                <TableCell className="font-mono text-xs">
                  <Link
                    to="/spaces/$spaceOwner/$spaceType"
                    params={params}
                    className="hover:underline"
                  >
                    {ref.spaceType}
                  </Link>
                </TableCell>
              )}
              {showOwner && (
                <TableCell
                  className="font-mono text-xs text-muted-foreground"
                  title={ref.spaceDid}
                >
                  <DidHoverCard did={ref.spaceDid}>
                    <Link
                      to="/spaces/$spaceOwner"
                      params={{ spaceOwner: ref.spaceDid }}
                      className="hover:underline"
                    >
                      {ref.spaceDid}
                    </Link>
                  </DidHoverCard>
                </TableCell>
              )}
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
