import CalendarVirtual from "@/components/CalendarVirtual";
import { CreateEventModal } from "@/components/CreateEventModal";
import { useLiveQuery } from "@tanstack/react-db";
import { createFileRoute } from "@tanstack/react-router";
import { Button, Calendar } from "internal/components/ui";
import { useState } from "react";

export const Route = createFileRoute("/_requireAuth/v2")({
  component: () => {
    const { eventCollection } = Route.useRouteContext();
    const [date, setDate] = useState(() => new Date());
    const { data: events } = useLiveQuery((q) =>
      q.from({ event: eventCollection }).select(({ event }) => {
        return {
          start: event.value.startsAt,
          end: event.value.endsAt,
          title: event.value.name,
        };
      }),
    );
    return (
      <div className="flex h-screen bg-(--border) gap-[1px]">
        <div className="overflow-hidden rounded-md flex-1">
          <CalendarVirtual date={date} events={events} />
        </div>
        <div className="w-84 rounded-md overflow-hidden bg-background flex flex-col">
          <CreateEventModal
            trigger={<Button className="mx-1">+ Create</Button>}
          />
          <Calendar
            className="w-full"
            captionLayout="dropdown"
            onMonthChange={setDate}
            onDayClick={setDate}
          />
          <Button onClick={() => setDate(new Date())} className="mx-1">
            Today
          </Button>
        </div>
      </div>
    );
  },
});
