import { RecordCard, RecordCardProps } from '@/components/RecordCard';
import { createFileRoute } from '@tanstack/react-router'
import { listPrivateRecords } from 'internal';

export const Route = createFileRoute('/_requireAuth/collections/$collection')({
    async loader({ context, params }) {
        const { authManager } = context;
        const did = authManager.getAuthInfo()!.did;
        const { collection } = params;

        const data = await listPrivateRecords(
            context.authManager,
            collection,
            undefined,
            undefined,
            [did],
            true,
        );

        // Collect unique DID grantees across all records
        const granteeDids = [
            ...new Set(
                data.records.flatMap((r) =>
                    (r.permissions ?? [])
                        .filter((p) => p.$type === 'network.habitat.grantee#didGrantee')
                        .map((p) => (p as { did: string }).did)
                )
            ),
        ];

        // Fetch Bluesky profiles for all DID grantees
        const profilesByDid: Record<string, { avatar?: string; handle: string }> = {};
        if (granteeDids.length > 0) {
            const headers = new Headers();
            headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
            const profileParams = new URLSearchParams();
            for (const granteeDid of granteeDids) profileParams.append("actors", granteeDid);
            const resp = await authManager.fetch(
                `/xrpc/app.bsky.actor.getProfiles?${profileParams.toString()}`,
                "GET",
                null,
                headers,
            );
            if (resp.ok) {
                const profileData: { profiles: { did: string; handle: string; avatar?: string }[] } =
                    await resp.json();
                for (const p of profileData.profiles) {
                    profilesByDid[p.did] = { avatar: p.avatar, handle: p.handle };
                }
            }
        }

        // Transform records into RecordCardProps[]
        const records: RecordCardProps[] = data.records.map((record) => ({
            uri: record.uri,
            value: record.value,
            grantees: (record.permissions ?? [])
                .filter((p) => p.$type === 'network.habitat.grantee#didGrantee')
                .map((p) => {
                    const granteeDid = (p as { did: string }).did;
                    return { did: granteeDid, ...profilesByDid[granteeDid] };
                }),
        }));

        return { records };
    },
    pendingComponent: () => {
        const { collection } = Route.useParams();
        return <p> Loading {collection}...</p>
    },
    component: CollectionRecords,
})

function CollectionRecords() {
    const { collection } = Route.useParams();
    const { records } = Route.useLoaderData();
    return <>
        <h2>{collection}</h2>
        <div className="grid">
            {records.map((record) => (
                <RecordCard key={record.uri} {...record} />
            ))}
        </div>
    </>
}
