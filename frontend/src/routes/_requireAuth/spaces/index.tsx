import {
  createFileRoute,
  Link,
  useNavigate,
  useRouter,
} from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { procedure, parseSpaceURI, SpaceURIParts } from "internal";
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
import { SpacesPageLayout } from "@/components/SpacesPageLayout";

export const Route = createFileRoute("/_requireAuth/spaces/")({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      spacesListQueryOptions(context.authManager),
    ),
  component: SpaceTypesList,
});

interface SpaceTypeSummary {
  parts: SpaceURIParts;
  spaceCount: number;
}

// summarizeByType rolls the flat space list up into one row per space type,
// which is all the landing page shows.
function summarizeByType(spaces: SpaceView[]): SpaceTypeSummary[] {
  const byType = new Map<string, SpaceTypeSummary>();
  for (const space of spaces) {
    const parts = parseSpaceURI(space.uri);
    if (!parts) continue;
    const summary = byType.get(parts.spaceType) ?? {
      parts: parts,
      spaceCount: 0,
    };
    summary.spaceCount += 1;
    byType.set(parts.spaceType, summary);
  }
  return [...byType.values()];
}

function SpaceTypesList() {
  const spaces = Route.useLoaderData();
  const types = summarizeByType(spaces);

  return (
    <SpacesPageLayout
      title="Spaces"
      subtitle="The spaces you have permission to, grouped by type."
    >
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
            </TableRow>
          </TableHeader>
          <TableBody>
            {types.map((summary) => (
              <TableRow key={summary.parts.spaceType}>
                <TableCell>
                  <Link
                    to="/spaces/type/$spaceType"
                    params={{ spaceType: summary.parts.spaceType }}
                    className="font-mono hover:underline"
                  >
                    {summary.parts.spaceType}
                  </Link>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {summary.spaceCount}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </SpacesPageLayout>
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
