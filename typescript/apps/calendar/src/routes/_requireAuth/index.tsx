import { useMutation } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  createEvent,
  deleteEvent,
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
      queryClient.fetchQuery({
        queryKey: ["events"],
        queryFn: () => listEvents(authManager),
      }),
      queryClient.fetchQuery({
        queryKey: ["invites"],
        queryFn: () => listInvites(authManager),
      }),
      queryClient.fetchQuery({
        queryKey: ["rsvps"],
        queryFn: () => listRsvps(authManager),
      }),
    ]);

    return { events, invites, rsvps };
  },
});

function CalendarPage() {
  const { authManager } = Route.useRouteContext();
  const router = useRouter();
  const userDid = authManager.getAuthInfo()?.did;

  const { events, invites, rsvps } = Route.useLoaderData();
  if (!userDid) throw new Error("User DID not found");

  // Create event modal state
  const [newEventData, setNewEventData] = useState<
    { startsAt: string; endsAt?: string } | undefined
  >(undefined);

  // Event details modal state
  const [selectedEvent, setSelectedEvent] = useState<{
    uri: string;
    cal: CalendarEvent;
  } | null>(null);

  // Edit event modal state
  const [editingEvent, setEditingEvent] = useState<{
    uri: string;
    cal: CalendarEvent;
  } | null>(null);

  const createEventMutation = useMutation({
    mutationFn: ({
      event,
      invitedDids,
    }: {
      event: CreateEventInput;
      invitedDids: string[];
    }) => createEvent(authManager, userDid, event, invitedDids),
    onSuccess: () => {
      router.invalidate();
      setNewEventData(undefined);
    },
  });

  const editEventMutation = useMutation({
    mutationFn: ({ event }: { event: CreateEventInput }) => {
      if (!editingEvent) throw new Error("No event to edit");
      const mergedEvent: CalendarEvent = {
        ...editingEvent.cal,
        name: event.name,
        description: event.description,
        startsAt: event.startsAt,
        endsAt: event.endsAt,
      };
      return editEvent(authManager, editingEvent.uri, mergedEvent);
    },
    onSuccess: () => {
      router.invalidate();
      setEditingEvent(null);
    },
  });

  const deleteEventMutation = useMutation({
    mutationFn: ({ eventUri }: { eventUri: string }) =>
      deleteEvent(authManager, eventUri),
    onSuccess: () => {
      router.invalidate();
      setSelectedEvent(null);
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
    setSelectedEvent({
      uri: eventUri,
      cal: event,
    });
  }

  function handleEdit(eventUri: string, event: CalendarEvent) {
    setSelectedEvent(null);
    setEditingEvent({ uri: eventUri, cal: event });
  }

  function handleDelete(eventUri: string) {
    deleteEventMutation.mutate({ eventUri });
  }

  function handleRsvp(eventUri: string, status: RsvpStatus) {
    rsvpMutation.mutate({ eventUri, status });
  }

  return (
    <div>
      <nav>
        <h1>Calendar</h1>
        <a href="https://habitat.network/habitat" target="_blank">
          🌱 Habitat Portal
        </a>
      </nav>

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
        event={selectedEvent?.cal ?? null}
        eventUri={selectedEvent?.uri ?? null}
        invites={invites}
        rsvps={rsvps}
        userDid={userDid}
        onClose={() => {
          setSelectedEvent(null);
        }}
        onRsvp={handleRsvp}
        isRsvpPending={rsvpMutation.isPending}
        onEdit={handleEdit}
        onDelete={handleDelete}
      />

      <CreateEventModal
        isOpen={editingEvent !== null}
        initialEvent={
          editingEvent
            ? {
              name: editingEvent.cal.name,
              description: editingEvent.cal.description,
              startsAt: editingEvent.cal.startsAt,
              endsAt: editingEvent.cal.endsAt,
            }
            : undefined
        }
        title="Edit Event"
        onClose={() => {
          setEditingEvent(null);
        }}
        onSubmit={(event) => editEventMutation.mutate({ event })}
        onCancel={() => {
          setEditingEvent(null);
        }}
        isPending={editEventMutation.isPending}
        error={editEventMutation.error ?? null}
      />
    </div>
  );
}
