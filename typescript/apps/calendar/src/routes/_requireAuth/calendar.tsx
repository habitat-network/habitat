import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  createEvent,
  listEvents,
  listInvites,
  listRsvps,
  createRsvp,
  type CalendarEvent,
  type InviteWithEvent,
  type RsvpStatus,
} from "../../controllers/eventController.ts";
import { CalendarView } from "../../components/CalendarView.tsx";
import { CreateEventModal } from "../../components/CreateEventModal.tsx";
import { EventDetailsModal } from "../../components/EventDetailsModal.tsx";
import type { CreateEventInput } from "../../components/EventForm.tsx";

export const Route = createFileRoute("/_requireAuth/calendar")({
  component: CalendarPage,
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const client = authManager.client();

    const [events, invites, rsvps] = await Promise.all([
      queryClient.ensureQueryData({
        queryKey: ["events"],
        queryFn: () => listEvents(client),
      }),
      queryClient.ensureQueryData({
        queryKey: ["invites"],
        queryFn: () => listInvites(client),
      }),
      queryClient.ensureQueryData({
        queryKey: ["rsvps"],
        queryFn: () => listRsvps(client),
      }),
    ]);

    return { events, invites, rsvps };
  },
});

function CalendarPage() {
  const { events } = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const client = authManager.client();
  const userDid = authManager.getAuthInfo()?.did;
  if (!userDid) throw new Error("User DID not found");

  const [selectedEvent, setSelectedEvent] = useState<
    { startsAt: string; endsAt?: string } | undefined
  >(undefined);

  // Event details modal state
  const [detailsModalOpen, setDetailsModalOpen] = useState(false);
  const [selectedEventUri, setSelectedEventUri] = useState<string | null>(null);
  const [selectedEvent, setSelectedEvent] = useState<CalendarEvent | null>(null);

  const createEventMutation = useMutation({
    mutationFn: ({
      event,
      invitedDids,
    }: {
      event: CreateEventInput;
      invitedDids: string[];
    }) => createEvent(client, userDid, event, invitedDids),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      queryClient.invalidateQueries({ queryKey: ["invites"] });
      queryClient.invalidateQueries({ queryKey: ["rsvps"] });
      router.invalidate();
      setSelectedEvent(undefined);
    },
  });

  const rsvpMutation = useMutation({
    mutationFn: ({ eventUri, status }: { eventUri: string; status: RsvpStatus }) =>
      createRsvp(client, eventUri, status),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["rsvps"] });
    },
  });

  function handleDateClick(data: { startsAt: string; endsAt?: string }) {
    setSelectedEvent(data);
  }

  function handleSelect(data: { startsAt: string; endsAt?: string }) {
    setSelectedEvent(data);
  }

  function handleSubmit(event: CreateEventInput, invitedDids: string[]) {
    createEventMutation.mutate({ event, invitedDids });
  }

  function handleEventClick(eventUri: string, event: CalendarEvent) {
    setSelectedEventUri(eventUri);
    setSelectedEvent(event);
    setDetailsModalOpen(true);
  }

  function handleRsvp(eventUri: string, status: RsvpStatus) {
    rsvpMutation.mutate({ eventUri, status });
  }

  return (
    <div>
      <header style={{ marginBottom: "1rem" }}>
        <nav>
          <ul>
            <li>
              <Link to="/">Events List</Link>
            </li>
            <li>
              <strong>Calendar View</strong>
            </li>
          </ul>
        </nav>
      </header>

      <h1>Calendar</h1>

      <CalendarView
        events={events.records}
        invites={invites}
        userDid={userDid}
        onDateClick={handleDateClick}
        onSelect={handleSelect}
        onEventClick={handleEventClick}
        emptyComponent={
          <p>
            No events with dates to display. <Link to="/">Create an event</Link>{" "}
            with a start date to see it here.
          </p>
        }
      />

      <CreateEventModal
        isOpen={selectedEvent !== undefined}
        initialEvent={selectedEvent}
        onClose={() => setSelectedEvent(undefined)}
        onSubmit={handleSubmit}
        onCancel={() => setSelectedEvent(undefined)}
        isPending={createEventMutation.isPending}
        error={createEventMutation.error ?? null}
      />

      <EventDetailsModal
        isOpen={detailsModalOpen}
        event={selectedEvent}
        eventUri={selectedEventUri}
        invites={invites}
        rsvps={rsvps}
        userDid={userDid}
        onClose={() => setDetailsModalOpen(false)}
        onRsvp={handleRsvp}
        isRsvpPending={rsvpMutation.isPending}
      />
    </div>
  );
}
