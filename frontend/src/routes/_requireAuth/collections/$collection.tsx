import { createFileRoute } from "@tanstack/react-router";
import {
  GranteeAvatars,
  listPrivateRecords,
  } from "internal";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import ReactJson from "react-json-view";

export const Route = createFileRoute("/_requireAuth/collections/$collection")({
  async loader({ context, params }) {
    const { authManager } = context;
    const did = authManager.getAuthInfo()!.did;
    const { collection } = params;

    const { records } = await listPrivateRecords(
      context.authManager,
      collection,
      undefined,
      undefined,
      [did],
      true,
    );

    return { records };
  },
  pendingComponent: () => {
    const { collection } = Route.useParams();
    return <p> Loading {collection}...</p>;
  },
  component: CollectionRecords,
});

function CollectionRecords() {
  const { collection } = Route.useParams();
  const { records } = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  return (
    <>
      <h2 className="text-2xl mb-4">{collection}</h2>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead />
            <TableHead>Shared with</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {records.map((record) => (
            <TableRow key={record.uri}>
              <TableCell>
                <div className="flex flex-col gap-2">
                  <span>{record.uri.split("/")[4]}</span>
                  <ReactJson src={record.value} />
                </div>
              </TableCell>
              <TableCell className="flex justify-start">
                <GranteeAvatars
                  authManager={authManager}
                  grantees={record.permissions}
                  uri={record.uri}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </>
  );
}
