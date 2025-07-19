import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute('/_requireAuth/permissions/people')({
    loader() {
        // fetch users
    },
    component() {
        return <>
            <h2>People</h2>
        </>
    }
})
