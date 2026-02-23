import { createFileRoute, Link } from "@tanstack/react-router";
import { listEvents, type CalendarEvent } from "../../controllers/eventController.ts";
import { CalendarView } from "../../components/CalendarView.tsx";
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
        emptyComponent={
          <p>
            No events with dates to display.{" "}
            <Link to="/">Create an event</Link> with a start date to see it
            here.
          </p>
        }
      />
    </div>
  );
}
