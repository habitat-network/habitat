import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { EventController } from "../../controllers/eventController.ts";

export const Route = createFileRoute("/_requireAuth/")({
  component: CalendarPage,
});

function CalendarPage() {
  const { authManager } = Route.useRouteContext();
  const eventController = useMemo(() => {
    const client = authManager.client();
    const did = authManager.did;
    if (!did) {
      throw new Error("User DID not found");
    }
    return new EventController(client, did);
  }, [authManager]);
  const queryClient = useQueryClient();

  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newEvent, setNewEvent] = useState({
    name: "",
    description: "",
    startsAt: "",
    endsAt: "",
    invitedDids: "",
  });

  const {
    data: rsvpData,
    isLoading: rsvpLoading,
    error: rsvpError,
  } = useQuery({
    queryKey: ["rsvps"],
    queryFn: () => eventController.listRsvps(),
  });

  const {
    data: eventData,
    isLoading: eventLoading,
    error: eventError,
  } = useQuery({
    queryKey: ["events"],
    queryFn: () => eventController.listEvents(),
  });

  // Fetch and process RSVP notifications on load
  useQuery({
    queryKey: ["rsvpNotifications"],
    queryFn: async () => {
      const notifications = await eventController.getRsvpNotifications();
      console.log("notifications", notifications);
      // Refetch RSVPs after processing notifications
      if (notifications.length > 0) {
        queryClient.invalidateQueries({ queryKey: ["rsvps"] });
      }
      return notifications;
    },
  });

  const createEventMutation = useMutation({
    mutationFn: (event: {
      name: string;
      description?: string;
      startsAt?: string;
      endsAt?: string;
      invitedDids: string[];
    }) => {
      const { invitedDids, ...eventData } = event;
      return eventController.createEvent(eventData, invitedDids);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      setShowCreateForm(false);
      setNewEvent({ name: "", description: "", startsAt: "", endsAt: "", invitedDids: "" });
    },
  });

  const handleCreateEvent = (e: React.FormEvent) => {
    e.preventDefault();
    const invitedDids = newEvent.invitedDids
      .split(",")
      .map((did) => did.trim())
      .filter((did) => did.length > 0);
    createEventMutation.mutate({
      name: newEvent.name,
      description: newEvent.description || undefined,
      startsAt: newEvent.startsAt || undefined,
      endsAt: newEvent.endsAt || undefined,
      invitedDids,
    });
  };

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
          <form onSubmit={handleCreateEvent} style={{ marginBottom: "1rem" }}>
            <div>
              <label>
                Name:
                <input
                  type="text"
                  value={newEvent.name}
                  onChange={(e) =>
                    setNewEvent({ ...newEvent, name: e.target.value })
                  }
                  required
                />
              </label>
            </div>
            <div>
              <label>
                Description:
                <input
                  type="text"
                  value={newEvent.description}
                  onChange={(e) =>
                    setNewEvent({ ...newEvent, description: e.target.value })
                  }
                />
              </label>
            </div>
            <div>
              <label>
                Starts At:
                <input
                  type="datetime-local"
                  value={newEvent.startsAt}
                  onChange={(e) =>
                    setNewEvent({ ...newEvent, startsAt: e.target.value })
                  }
                />
              </label>
            </div>
            <div>
              <label>
                Ends At:
                <input
                  type="datetime-local"
                  value={newEvent.endsAt}
                  onChange={(e) =>
                    setNewEvent({ ...newEvent, endsAt: e.target.value })
                  }
                />
              </label>
            </div>
            <div>
              <label>
                Invite (comma-separated DIDs):
                <input
                  type="text"
                  value={newEvent.invitedDids}
                  onChange={(e) =>
                    setNewEvent({ ...newEvent, invitedDids: e.target.value })
                  }
                  placeholder="did:plc:abc123, did:plc:xyz789"
                />
              </label>
            </div>
            <button type="submit" disabled={createEventMutation.isPending}>
              {createEventMutation.isPending ? "Creating..." : "Save Event"}
            </button>
            {createEventMutation.isError && (
              <p style={{ color: "red" }}>
                Error: {createEventMutation.error.message}
              </p>
            )}
          </form>
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
