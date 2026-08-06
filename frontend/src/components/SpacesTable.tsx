import { Link } from "@tanstack/react-router";
import { parseSpaceURI } from "internal";
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
          const parts = parseSpaceURI(space.uri);
          if (!parts) return null;
          return (
            <TableRow key={space.uri}>
              <TableCell className="font-mono">
                <Link
                  to="/spaces/$spaceOwner/$spaceType/$spaceKey"
                  params={parts}
                  className="hover:underline"
                >
                  {parts.spaceKey}
                </Link>
              </TableCell>
              {showType && (
                <TableCell className="font-mono text-xs">
                  <Link
                    to="/spaces/$spaceOwner/$spaceType"
                    params={parts}
                    className="hover:underline"
                  >
                    {parts.spaceType}
                  </Link>
                </TableCell>
              )}
              {showOwner && (
                <TableCell
                  className="font-mono text-xs text-muted-foreground"
                  title={parts?.spaceOwner}
                >
                  <Link
                    to="/spaces/$spaceOwner"
                    params={{ spaceOwner: parts.spaceOwner }}
                    className="hover:underline"
                  >
                    {parts.spaceOwner}
                  </Link>
                </TableCell>
              )}
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
