import FullCalendar from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/daygrid";
import interactionPlugin from "@fullcalendar/interaction";
import type { EventInput } from "@fullcalendar/core";
import type { DateClickArg } from "@fullcalendar/interaction";
import type { DateSelectArg } from "@fullcalendar/core";
import type { CalendarEvent } from "../controllers/eventController.ts";

export interface EventRecord {
  uri: string;
  cid: string;
  value: CalendarEvent;
}

export interface CreateEventInitialData {
  startsAt: string;
  endsAt?: string;
}

interface CalendarViewProps {
  /** Lexicon event records from listEvents. Adapts to FullCalendar schema internally. */
  events: EventRecord[];
  /** Rendered when no events have both name and startsAt. */
  emptyComponent?: React.ReactNode;
  /** Called when user clicks a date. Receives ISO start string and allDay flag. */
  onDateClick?: (data: CreateEventInitialData) => void;
  /** Called when user drag-selects a time range. Receives ISO start and end strings. */
  onSelect?: (data: CreateEventInitialData) => void;
}

// Adapts community.lexicon.calendar.event to FullCalendar event input.
function communityLexicontFullCallendarEventAdapter(
  record: EventRecord,
): EventInput {
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

export function CalendarView({
  events,
  emptyComponent,
  onDateClick,
  onSelect,
}: CalendarViewProps) {
  const displayable = events.filter(isDisplayable);
  const fullCalendarEvents: EventInput[] = displayable.map(
    communityLexicontFullCallendarEventAdapter,
  );

  if (displayable.length === 0 && emptyComponent) {
    return <>{emptyComponent}</>;
  }

  function handleDateClick(info: DateClickArg) {
    const start = info.date;
    onDateClick?.({
      startsAt: start.toISOString(),
      endsAt: info.allDay ? addHours(start, 1).toISOString() : undefined,
    });
  }

  function handleSelect(info: DateSelectArg) {
    onSelect?.({
      startsAt: info.start.toISOString(),
      endsAt: info.end.toISOString(),
    });
  }

  return (
    <FullCalendar
      plugins={[interactionPlugin, dayGridPlugin]}
      initialView="dayGridMonth"
      events={fullCalendarEvents}
      selectable={Boolean(onSelect)}
      dateClick={onDateClick ? handleDateClick : undefined}
      select={onSelect ? handleSelect : undefined}
      headerToolbar={{
        left: "prev,next today",
        center: "title",
        right: "dayGridMonth,dayGridWeek",
      }}
      height="auto"
    />
  );
}

function addHours(date: Date, hours: number): Date {
  const result = new Date(date);
  result.setTime(result.getTime() + hours * 60 * 60 * 1000);
  return result;
}
