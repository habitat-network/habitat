import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  createEvent,
  editEvent,
  listEvents,
  listInvites,
  listRsvps,
  createRsvp,
  type CalendarEvent,
  type RsvpStatus,
} from "../../controllers/eventController.ts";
import { CalendarView } from "../../components/CalendarView.tsx";
import { CreateEventModal } from "../../components/CreateEventModal.tsx";
import { EventDetailsModal } from "../../components/EventDetailsModal.tsx";
import type { CreateEventInput } from "../../components/EventForm.tsx";

export const Route = createFileRoute("/_requireAuth/")({
  component: CalendarPage,
  async loader({ context }) {
    const { authManager, queryClient } = context;

    const [events, invites, rsvps] = await Promise.all([
      queryClient.ensureQueryData({
        queryKey: ["events"],
        queryFn: () => listEvents(authManager),
      }),
      queryClient.ensureQueryData({
        queryKey: ["invites"],
        queryFn: () => listInvites(authManager),
      }),
      queryClient.ensureQueryData({
        queryKey: ["rsvps"],
        queryFn: () => listRsvps(authManager),
      }),
    ]);

    return { events, invites, rsvps };
  },
});

function CalendarPage() {
  const { events, invites, rsvps } = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const userDid = authManager.getAuthInfo()?.did;
  if (!userDid) throw new Error("User DID not found");

  // Create event modal state
  const [newEventData, setNewEventData] = useState<
    { startsAt: string; endsAt?: string } | undefined
  >(undefined);

  // Event details modal state
  const [selectedEventUri, setSelectedEventUri] = useState<string | null>(null);
  const [selectedEvent, setSelectedEvent] = useState<CalendarEvent | null>(
    null,
  );

  // Edit event modal state
  const [editingEventUri, setEditingEventUri] = useState<string | null>(null);
  const [editingEvent, setEditingEvent] = useState<CalendarEvent | null>(null);

  const createEventMutation = useMutation({
    mutationFn: ({
      event,
      invitedDids,
    }: {
      event: CreateEventInput;
      invitedDids: string[];
    }) => createEvent(authManager, userDid, event, invitedDids),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      queryClient.invalidateQueries({ queryKey: ["invites"] });
      queryClient.invalidateQueries({ queryKey: ["rsvps"] });
      router.invalidate();
      setNewEventData(undefined);
    },
  });

  const editEventMutation = useMutation({
    mutationFn: ({ event }: { event: CreateEventInput }) => {
      if (!editingEventUri || !editingEvent) throw new Error("No event to edit");
      const mergedEvent: CalendarEvent = {
        ...editingEvent,
        name: event.name,
        description: event.description,
        startsAt: event.startsAt,
        endsAt: event.endsAt,
      };
      return editEvent(authManager, editingEventUri, mergedEvent);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["events"] });
      router.invalidate();
      setEditingEvent(null);
      setEditingEventUri(null);
    },
  });

  const rsvpMutation = useMutation({
    mutationFn: ({
      eventUri,
      status,
    }: {
      eventUri: string;
      status: RsvpStatus;
    }) => createRsvp(authManager, eventUri, status),
    onSuccess: () => {
      router.invalidate();
      queryClient.invalidateQueries({ queryKey: ["rsvps"] });
    },
  });

  function handleDateClick(data: { startsAt: string; endsAt?: string }) {
    setNewEventData(data);
  }

  function handleSelect(data: { startsAt: string; endsAt?: string }) {
    setNewEventData(data);
  }

  function handleSubmit(event: CreateEventInput, invitedDids: string[]) {
    createEventMutation.mutate({ event, invitedDids });
  }

  function handleEventClick(eventUri: string, event: CalendarEvent) {
    setSelectedEventUri(eventUri);
    setSelectedEvent(event);
  }

  function handleEdit(eventUri: string, event: CalendarEvent) {
    setSelectedEvent(null);
    setSelectedEventUri(null);
    setEditingEventUri(eventUri);
    setEditingEvent(event);
  }

  function handleRsvp(eventUri: string, status: RsvpStatus) {
    rsvpMutation.mutate({ eventUri, status });
  }

  return (
    <div>
      <h1>Calendar</h1>

      <CalendarView
        events={events.records}
        invites={invites}
        userDid={userDid}
        onDateClick={handleDateClick}
        onSelect={handleSelect}
        onEventClick={handleEventClick}
      />

      <CreateEventModal
        isOpen={newEventData !== undefined}
        initialEvent={newEventData}
        onClose={() => setNewEventData(undefined)}
        onSubmit={handleSubmit}
        onCancel={() => setNewEventData(undefined)}
        isPending={createEventMutation.isPending}
        error={createEventMutation.error ?? null}
      />

      <EventDetailsModal
        isOpen={selectedEvent !== null}
        event={selectedEvent}
        eventUri={selectedEventUri}
        invites={invites}
        rsvps={rsvps}
        userDid={userDid}
        onClose={() => {
          setSelectedEvent(null);
          setSelectedEventUri(null);
        }}
        onRsvp={handleRsvp}
        isRsvpPending={rsvpMutation.isPending}
        onEdit={handleEdit}
      />

      <CreateEventModal
        isOpen={editingEvent !== null}
        initialEvent={
          editingEvent
            ? {
              name: editingEvent.name,
              description: editingEvent.description,
              startsAt: editingEvent.startsAt,
              endsAt: editingEvent.endsAt,
            }
            : undefined
        }
        title="Edit Event"
        onClose={() => {
          setEditingEvent(null);
          setEditingEventUri(null);
        }}
        onSubmit={(event) => editEventMutation.mutate({ event })}
        onCancel={() => {
          setEditingEvent(null);
          setEditingEventUri(null);
        }}
        isPending={editEventMutation.isPending}
        error={editEventMutation.error ?? null}
      />
    </div>
  );
}
