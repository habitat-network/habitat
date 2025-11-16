import { listPermissions } from "@/queries/permissions";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

interface Data {
    did: string;
}

export const Route = createFileRoute(
    "/_requireAuth/permissions/lexicons/$lexiconId",
)({
    async loader({ context, params }) {
        const response = await context.queryClient.fetchQuery(
            listPermissions(context.authManager),
        );
        return response[params.lexiconId];
    },
    component() {
        const router = useRouter();
        const { authManager } = Route.useRouteContext();
        const params = Route.useParams();
        const people = Route.useLoaderData();
        const form = useForm<Data>({});
        const { mutate: add, isPending: isAdding } = useMutation({
            async mutationFn(data: Data) {
                await authManager?.fetch(
                    `/xrpc/com.habitat.addPermission`,
                    "POST",
                    JSON.stringify({
                        did: data.did,
                        lexicon: params.lexiconId,
                    }),
                );
                form.reset();
                router.invalidate();
            },
            onError(e) {
                console.error(e);
            },
        });

        const { mutate: remove } = useMutation({
            async mutationFn(data: Data) {
                // remove permission
                await authManager?.fetch(
                    `/xrpc/com.habitat.removePermission`,
                    "POST",
                    JSON.stringify({
                        did: data.did,
                        lexicon: params.lexiconId,
                    }),
                );
                router.invalidate();
            },
            onError(e) {
                console.error(e);
            },
        });
        return (
            <>
                <h3>{params.lexiconId}</h3>
                <form onSubmit={form.handleSubmit((data) => add(data))}>
                    <fieldset role="group">
                        <input type="text" {...form.register("did")} />
                        <button type="submit" aria-busy={isAdding}>
                            Add
                        </button>
                    </fieldset>
                </form>
                <table>
                    <thead>
                        <tr>
                            <th>Person</th>
                            <th />
                        </tr>
                    </thead>
                    <tbody>
                        {people?.map((person) => (
                            <tr key={person}>
                                <td>{person}</td>
                                <td>
                                    <button type="button" onClick={() => remove({ did: person })}>
                                        🗑️
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </>
        );
    },
});
