import {
  createFileRoute,
  Link,
  useNavigate,
  useRouter,
} from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { procedure, parseSpaceURI } from "internal";
import {
  Button,
  Card,
  CardContent,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
} from "internal/components/ui";
import { spacesListQueryOptions, type SpaceView } from "@/queries/spaces";

export const Route = createFileRoute("/_requireAuth/spaces/")({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      spacesListQueryOptions(context.authManager),
    ),
  pendingComponent: () => <p className="py-8">Loading spaces…</p>,
  component: SpaceTypesList,
});

interface SpaceTypeSummary {
  type: string;
  spaceCount: number;
  memberCount: number;
}

// summarizeByType rolls the flat space list up into one row per space type,
// which is all the landing page shows.
function summarizeByType(spaces: SpaceView[]): SpaceTypeSummary[] {
  const byType = new Map<string, SpaceTypeSummary>();
  for (const space of spaces) {
    const summary = byType.get(space.type) ?? {
      type: space.type,
      spaceCount: 0,
      memberCount: 0,
    };
    summary.spaceCount += 1;
    summary.memberCount += space.memberCount ?? 0;
    byType.set(space.type, summary);
  }
  return [...byType.values()].sort((a, b) => a.type.localeCompare(b.type));
}

function SpaceTypesList() {
  const spaces = Route.useLoaderData();
  const types = summarizeByType(spaces);

  return (
    <div className="flex flex-col gap-6 py-6">
      <div>
        <h1 className="text-2xl font-semibold">Spaces</h1>
        <p className="text-muted-foreground text-sm">
          The spaces you have permission to, grouped by type.
        </p>
      </div>

      <CreateSpaceForm />

      {types.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            You aren’t in any spaces yet. Create one to get started.
          </CardContent>
        </Card>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Type</TableHead>
              <TableHead className="text-right">Spaces</TableHead>
              <TableHead className="text-right">Members</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {types.map((summary) => (
              <TableRow key={summary.type}>
                <TableCell>
                  <Link
                    to="/spaces/type/$spaceType"
                    params={{ spaceType: summary.type }}
                    className="font-mono hover:underline"
                  >
                    {summary.type}
                  </Link>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {summary.spaceCount}
                </TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">
                  {summary.memberCount}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function CreateSpaceForm() {
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();
  const [spaceType, setSpaceType] = useState("");

  const { mutate: createSpace, isPending } = useMutation({
    async mutationFn(type: string) {
      const { uri } = await procedure(
        "network.habitat.space.createSpace",
        { type },
        { authManager },
      );
      return uri;
    },
    async onSuccess(uri) {
      setSpaceType("");
      await queryClient.invalidateQueries(spacesListQueryOptions(authManager));
      await router.invalidate();
      const parts = parseSpaceURI(uri);
      if (parts) {
        await navigate({
          to: "/spaces/$spaceOwner/$spaceType/$spaceKey",
          params: parts,
        });
      }
    },
    onError(error) {
      toast.add({ title: "Couldn't create space", description: error.message });
    },
  });

  return (
    <form
      className="flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (spaceType.trim()) createSpace(spaceType.trim());
      }}
    >
      <Input
        value={spaceType}
        onChange={(e) => setSpaceType(e.target.value)}
        placeholder="network.habitat.example.thing"
        className="max-w-96 font-mono"
      />
      <Button type="submit" disabled={isPending || !spaceType.trim()}>
        Create space
      </Button>
    </form>
  );
}
