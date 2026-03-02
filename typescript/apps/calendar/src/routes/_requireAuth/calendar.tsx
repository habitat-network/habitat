import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import React, { useState } from "react";
import { createEvent, listEvents } from "../../controllers/eventController.ts";
import { CalendarView } from "../../components/CalendarView.tsx";
import { CreateEventModal } from "../../components/CreateEventModal.tsx";
import type { CreateEventInput } from "../../components/EventForm.tsx";

export const Route = createFileRoute("/_requireAuth/calendar")({
  component: CalendarPage,
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const client = authManager.client();

    const events = await queryClient.ensureQueryData({
      queryKey: ["events"],
      queryFn: () => listEvents(client),
    });

    return { events };
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
      router.invalidate();
      setSelectedEvent(undefined);
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
        onDateClick={handleDateClick}
        onSelect={handleSelect}
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
    </div>
  );
}
