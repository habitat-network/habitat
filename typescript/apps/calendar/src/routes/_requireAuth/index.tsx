import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  createEvent,
  listEvents,
  listRsvps,
} from "../../controllers/eventController.ts";
import {
  CreateEventForm,
  type CreateEventFormData,
} from "../../components/CreateEventForm.tsx";

export const Route = createFileRoute("/_requireAuth/")({
  component: CalendarPage,
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const client = authManager.client();

    // Fetch everything we need - "fetch the world"
    // listPrivateRecords now returns all accessible records (own + shared via notifications)
    const [rsvps, events] = await Promise.all([
      queryClient.ensureQueryData({
        queryKey: ["rsvps"],
        queryFn: () => listRsvps(client),
      }),
      queryClient.ensureQueryData({
        queryKey: ["events"],
        queryFn: () => listEvents(client),
      }),
    ]);
    return { rsvps, events };
  },
});

function CalendarPage() {
  const { authManager } = Route.useRouteContext();
  const router = useRouter();
  const { rsvps, events } = Route.useLoaderData();
  const client = authManager.client();
  const userDid = authManager.getAuthInfo()?.did;
  if (!userDid) {
    throw new Error("User DID not found");
  }
  const queryClient = useQueryClient();

  const [showCreateForm, setShowCreateForm] = useState(false);

  const createEventMutation = useMutation({
    mutationFn: (data: CreateEventFormData) => {
      const invitedDids = data.invitedDids
        .split(",")
        .map((d) => d.trim())
        .filter((d) => d.length > 0);
      return createEvent(
        client,
        userDid,
        {
          name: data.name,
          description: data.description || undefined,
          startsAt: data.startsAt || undefined,
          endsAt: data.endsAt || undefined,
        },
        invitedDids,
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      router.invalidate();
      setShowCreateForm(false);
    },
  });

  return (
    <div>
      <h1>Calendar</h1>

      <section>
        <div style={{ display: "flex", alignItems: "center", gap: "1rem" }}>
          <h2>Events</h2>
          <button
            type="button"
            onClick={() => setShowCreateForm(!showCreateForm)}
          >
            {showCreateForm ? "Cancel" : "Create Event"}
          </button>
        </div>

        {showCreateForm && (
          <CreateEventForm
            onSubmit={(data) => createEventMutation.mutate(data)}
            isPending={createEventMutation.isPending}
            error={createEventMutation.error}
          />
        )}

        {events.records.length === 0 ? (
          <p>No events found</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Description</th>
                <th>Starts</th>
                <th>Ends</th>
              </tr>
            </thead>
            <tbody>
              {events.records
                .filter((record) => {
                  if (!record.value?.name) {
                    console.error(
                      "Invalid event format, missing name:",
                      record.uri,
                    );
                    return false;
                  }
                  return true;
                })
                .map((record) => (
                  <tr key={record.uri}>
                    <td>{record.value.name}</td>
                    <td>{record.value.description || "-"}</td>
                    <td>
                      {record.value.startsAt
                        ? new Date(record.value.startsAt).toLocaleString()
                        : "-"}
                    </td>
                    <td>
                      {record.value.endsAt
                        ? new Date(record.value.endsAt).toLocaleString()
                        : "-"}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        )}
      </section>

      <section>
        <h2>RSVPs</h2>
        {rsvps.length === 0 ? (
          <p>No RSVPs found</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Event</th>
                <th>When</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {rsvps.map((rsvpWithEvent) => {
                // Format status - strip lexicon prefix if present
                const rawStatus = rsvpWithEvent.rsvp.status ?? "-";
                const status = rawStatus.includes("#")
                  ? rawStatus.split("#")[1]
                  : rawStatus;

                return (
                  <tr key={rsvpWithEvent.uri}>
                    <td>{rsvpWithEvent.event?.name ?? "Unknown Event"}</td>
                    <td>
                      {rsvpWithEvent.event?.startsAt
                        ? `${new Date(rsvpWithEvent.event.startsAt).toLocaleString()}${rsvpWithEvent.event.endsAt ? ` - ${new Date(rsvpWithEvent.event.endsAt).toLocaleString()}` : ""}`
                        : "-"}
                    </td>
                    <td>{status}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
