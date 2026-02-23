import FullCalendar from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/daygrid";
import type { EventInput } from "@fullcalendar/core";
import type { CalendarEvent } from "../controllers/eventController.ts";

export interface EventRecord {
  uri: string;
  cid: string;
  value: CalendarEvent;
}

interface CalendarViewProps {
  /** Lexicon event records from listEvents. Adapts to FullCalendar schema internally. */
  events: EventRecord[];
  /** Rendered when no events have both name and startsAt. */
  emptyComponent?: React.ReactNode;
}

// Adapts community.lexicon.calendar.event to FullCalendar event input.
function communityLexicontFullCallendarEventAdapter(record: EventRecord): EventInput {
  const { uri, cid, value } = record;
  return {
    id: uri,
    title: value.name,
    start: value.startsAt!,
    end: value.endsAt,
    extendedProps: {
      description: value.description,
      cid,
    },
  };
}

function isDisplayable(record: EventRecord): boolean {
  if (!record.value?.name) {
    console.error("Invalid event format, missing name:", record.uri);
    return false;
  }
  if (!record.value?.startsAt) {
    return false;
  }
  return true;
}

export function CalendarView({ events, emptyComponent }: CalendarViewProps) {
  const displayable = events.filter(isDisplayable);
  const fullCalendarEvents: EventInput[] = displayable.map(communityLexicontFullCallendarEventAdapter);

  if (displayable.length === 0 && emptyComponent) {
    return <>{emptyComponent}</>;
  }

  return (
    <FullCalendar
      plugins={[dayGridPlugin]}
      initialView="dayGridMonth"
      events={fullCalendarEvents}
      headerToolbar={{
        left: "prev,next today",
        center: "title",
        right: "dayGridMonth,dayGridWeek",
      }}
      height="auto"
    />
  );
}
