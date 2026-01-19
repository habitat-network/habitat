import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { createEvent, getRsvpNotifications, listEvents, listRsvps } from "../../controllers/eventController.ts";
import { CreateEventForm, type CreateEventFormData } from "../../components/CreateEventForm.tsx";

export const Route = createFileRoute("/_requireAuth/")({
  component: CalendarPage,
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const client = authManager.client();

    await Promise.all([
      queryClient.ensureQueryData({
        queryKey: ["rsvps"],
        queryFn: () => listRsvps(client),
      }),
      queryClient.ensureQueryData({
        queryKey: ["rsvpNotifications"],
        queryFn: () => getRsvpNotifications(client),
      }),
    ]);
  },
});

function CalendarPage() {
  const { authManager } = Route.useRouteContext();
  const client = authManager.client();
  const userDid = authManager.did;
  if (!userDid) {
    throw new Error("User DID not found");
  }
  const queryClient = useQueryClient();

  const [showCreateForm, setShowCreateForm] = useState(false);

  const {
    data: rsvpData,
    isLoading: rsvpLoading,
    error: rsvpError,
  } = useQuery({
    queryKey: ["rsvps"],
    queryFn: () => listRsvps(client),
  });

  const {
    data: eventData,
    isLoading: eventLoading,
    error: eventError,
  } = useQuery({
    queryKey: ["events"],
    queryFn: () => listEvents(client),
  });

  // Fetch and process RSVP notifications on load
  useQuery({
    queryKey: ["rsvpNotifications"],
    queryFn: async () => {
      const notifications = await getRsvpNotifications(client);
      console.log("notifications", notifications);
      // Refetch RSVPs after processing notifications
      if (notifications.length > 0) {
        queryClient.invalidateQueries({ queryKey: ["rsvps"] });
      }
      return notifications;
    },
  });

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
        invitedDids
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      setShowCreateForm(false);
    },
  });

  const isLoading = rsvpLoading || eventLoading;
  const error = rsvpError || eventError;

  if (isLoading) {
    return <div>Loading...</div>;
  }

  if (error) {
    return <div>Error: {error.message}</div>;
  }

  return (
    <div>
      <h1>Calendar</h1>

      <section>
        <div style={{ display: "flex", alignItems: "center", gap: "1rem" }}>
          <h2>Events</h2>
          <button type="button" onClick={() => setShowCreateForm(!showCreateForm)}>
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

        {eventData?.records.length === 0 ? (
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
              {eventData?.records
                .filter((record) => {
                  if (!record.value?.name) {
                    console.error("Invalid event format, missing name:", record.uri);
                    return false;
                  }
                  return true;
                })
                .map((record) => (
                  <tr key={record.uri}>
                    <td>{record.value.name}</td>
                    <td>{record.value.description || "-"}</td>
                    <td>{record.value.startsAt ? new Date(record.value.startsAt).toLocaleString() : "-"}</td>
                    <td>{record.value.endsAt ? new Date(record.value.endsAt).toLocaleString() : "-"}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        )}
      </section>

      <section>
        <h2>RSVPs</h2>
        {rsvpData?.length === 0 ? (
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
              {rsvpData?.map((rsvpWithEvent) => {
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
