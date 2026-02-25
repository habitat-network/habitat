import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  createEvent,
  listEvents,
  type CalendarEvent,
} from "../../controllers/eventController.ts";
import { CalendarView } from "../../components/CalendarView.tsx";
import { CreateEventModal } from "../../components/CreateEventModal.tsx";
import type { CreateEventInput } from "../../components/EventForm.tsx";
import type { ListPrivateRecordsResponse } from "internal/habitatClient.ts";

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

type LoaderData = {
  events: ListPrivateRecordsResponse<CalendarEvent>;
};

function CalendarPage() {
  const { events } = Route.useLoaderData() as LoaderData;
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();
  const client = authManager.client();
  const userDid = authManager.did;
  if (!userDid) throw new Error("User DID not found");

  const [modalOpen, setModalOpen] = useState(false);
  const [initialEvent, setInitialEvent] = useState<
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
      setModalOpen(false);
      setInitialEvent(undefined);
    },
  });

  function handleDateClick(data: { startsAt: string; endsAt?: string }) {
    setInitialEvent(data);
    setModalOpen(true);
  }

  function handleSelect(data: { startsAt: string; endsAt?: string }) {
    setInitialEvent(data);
    setModalOpen(true);
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
            No events with dates to display.{" "}
            <Link to="/">Create an event</Link> with a start date to see it
            here.
          </p>
        }
      />

      <CreateEventModal
        isOpen={modalOpen}
        initialEvent={initialEvent}
        onClose={() => setModalOpen(false)}
        onSubmit={handleSubmit}
        onCancel={() => setModalOpen(false)}
        isPending={createEventMutation.isPending}
        error={createEventMutation.error ?? null}
      />
    </div>
  );
}
