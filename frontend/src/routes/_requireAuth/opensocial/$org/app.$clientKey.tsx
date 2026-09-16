import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  clientMetadataQueryOptions,
  decodeAppAccessRkey,
  orgAppAccessQueryOptions,
} from "@/queries/opensocial";
import { describeScopes, type ScopeDescription } from "@/lib/oauthScopes";
import {
  Badge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "internal/components/ui";

export const Route = createFileRoute(
  "/_requireAuth/opensocial/$org/app/$clientKey",
)({
  component: AppDetail,
});

function AppDetail() {
  const { org, clientKey } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const clientId = decodeAppAccessRkey(clientKey);
  const { data, isLoading, error } = useQuery(
    clientMetadataQueryOptions(clientId),
  );
  const { data: appAccess = [] } = useQuery(
    orgAppAccessQueryOptions(org, authManager, queryClient),
  );
  const grantedScopes =
    appAccess.find((app) => app.rkey === clientKey)?.scopes ?? [];

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-4">
        <div className="flex flex-row items-center gap-3">
          {data?.metadata.logo_uri && (
            <img
              src={data.metadata.logo_uri}
              alt=""
              className="size-10 rounded-md border"
            />
          )}
          <div>
            <h2 className="text-base font-semibold">
              {data?.metadata.client_name ?? clientId}
            </h2>
            <p className="font-mono text-xs text-muted-foreground break-all">
              {clientId}
            </p>
          </div>
        </div>
        <div className="flex flex-col gap-2 text-sm">
          {isLoading && (
            <p className="text-muted-foreground">Loading client metadata…</p>
          )}
          {error && (
            <p className="text-destructive">
              Couldn't load this app's client metadata: {error.message}
            </p>
          )}
          {data?.loopback && (
            <p className="text-muted-foreground">
              This is a local development client — its metadata is decoded from
              the client_id itself rather than fetched.
            </p>
          )}
          {data && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
              {data.metadata.client_uri && (
                <>
                  <dt className="text-muted-foreground">Website</dt>
                  <dd>
                    <a
                      href={data.metadata.client_uri}
                      target="_blank"
                      rel="noreferrer"
                      className="hover:underline"
                    >
                      {data.metadata.client_uri}
                    </a>
                  </dd>
                </>
              )}
              {data.metadata.policy_uri && (
                <>
                  <dt className="text-muted-foreground">Privacy policy</dt>
                  <dd>
                    <a
                      href={data.metadata.policy_uri}
                      target="_blank"
                      rel="noreferrer"
                      className="hover:underline"
                    >
                      {data.metadata.policy_uri}
                    </a>
                  </dd>
                </>
              )}
              {data.metadata.tos_uri && (
                <>
                  <dt className="text-muted-foreground">Terms of service</dt>
                  <dd>
                    <a
                      href={data.metadata.tos_uri}
                      target="_blank"
                      rel="noreferrer"
                      className="hover:underline"
                    >
                      {data.metadata.tos_uri}
                    </a>
                  </dd>
                </>
              )}
            </dl>
          )}
        </div>
      </div>

      {data && (
        <div className="flex flex-col gap-4">
          <h2 className="text-base font-semibold">Access</h2>
          <Tabs defaultValue="requested">
            <TabsList>
              <TabsTrigger value="requested">App can request</TabsTrigger>
              <TabsTrigger value="granted">Granted to org</TabsTrigger>
            </TabsList>
            <TabsContent value="requested" className="pt-4">
              <ScopeList
                scopes={describeScopes(data.metadata.scope)}
                emptyMessage="This app requests no scopes beyond basic authentication."
              />
            </TabsContent>
            <TabsContent value="granted" className="pt-4">
              <ScopeList
                scopes={describeScopes(grantedScopes.join(" "))}
                emptyMessage="This app hasn't been granted an org credential, or was granted one before scope tracking existed."
              />
            </TabsContent>
          </Tabs>
        </div>
      )}
    </div>
  );
}

function ScopeList({
  scopes,
  emptyMessage,
}: {
  scopes: ScopeDescription[];
  emptyMessage: string;
}) {
  if (scopes.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyMessage}</p>;
  }
  return (
    <ul className="flex flex-col gap-3">
      {scopes.map((s) => (
        <li key={s.scope} className="flex flex-col gap-0.5">
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="font-mono text-xs">
              {s.scope}
            </Badge>
            <span className="font-medium text-sm">{s.summary}</span>
          </div>
          {s.detail && (
            <p className="text-xs text-muted-foreground">{s.detail}</p>
          )}
        </li>
      ))}
    </ul>
  );
}
