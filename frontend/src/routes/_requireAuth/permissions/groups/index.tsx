import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute('/_requireAuth/permissions/groups/')({
    loader() {
        // fetch groups
    },
    component() {
        return <>
            <h2>Groups</h2>
            <table>
            </table>
        </>
    }
})
