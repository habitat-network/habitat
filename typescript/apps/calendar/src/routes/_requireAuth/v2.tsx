import CalendarVirtual from "@/components/CalendarVirtual";
import ConnectGoogleDialog from "@/components/ConnectGoogleDialog";
import { CreateEventModal } from "@/components/CreateEventModal";
import { createEvent } from "@/controllers/eventController";
import { useLiveQuery } from "@tanstack/react-db";
import { createFileRoute } from "@tanstack/react-router";
import { Button, Calendar } from "internal/components/ui";
import { useState } from "react";

export const Route = createFileRoute("/_requireAuth/v2")({
  loader: async ({ context }) => { },
  component: () => {
    const { eventCollection, authManager } = Route.useRouteContext();
    const [date, setDate] = useState(() => new Date());

    const { data: events } = useLiveQuery((q) =>
      q.from({ event: eventCollection }),
    );

    return (
      <div className="flex h-screen bg-(--border) gap-[1px]">
        <div className="overflow-hidden rounded-md flex-1">
          <CalendarVirtual date={date} events={events} />
        </div>
        <div className="w-84 rounded-md overflow-hidden bg-background flex flex-col">
          <CreateEventModal
            trigger={<Button className="mx-1">+ Create</Button>}
            onSubmit={async (event, invitees) => {
              const { uri } = await createEvent(
                authManager,
                authManager.getAuthInfo()!.did,
                event,
                invitees,
              );
              eventCollection.insert({
                uri: uri,
                color: "#3b82f6",
                source: "pear",
                start: event.startsAt!,
                end: event.endsAt!,
                title: event.name,
              });
            }}
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
          <ConnectGoogleDialog />
        </div>
      </div>
    );
  },
});
