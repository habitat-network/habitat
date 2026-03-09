import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_requireAuth/collections/$collection')({
    component: RouteComponent,
})

function RouteComponent() {
    return <div>Hello "/_requireAuth/collections/$collection"!</div>
}
