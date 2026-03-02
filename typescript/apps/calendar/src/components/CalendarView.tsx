import FullCalendar from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/daygrid";
import interactionPlugin from "@fullcalendar/interaction";
import type { EventInput, DateSelectArg, EventClickArg } from "@fullcalendar/core";
import type { DateClickArg } from "@fullcalendar/interaction";
import type { DateSelectArg } from "@fullcalendar/core";
import {
  buildEventDataMap,
  getDisplayableInvites,
  type CalendarEvent,
  type InviteWithEvent,
  type EventRecord,
} from "../controllers/eventController.ts";

export type { EventRecord };

export interface CreateEventInitialData {
  startsAt: string;
  endsAt?: string;
}

interface CalendarViewProps {
  /** Lexicon event records from listEvents. Adapts to FullCalendar schema internally. */
  events: EventRecord[];
  /** Invites the user has received (with associated events). */
  invites?: InviteWithEvent[];
  /** The current user's DID, used to filter invites for display. */
  userDid?: string;
  /** Rendered when no events have both name and startsAt. */
  emptyComponent?: React.ReactNode;
  /** Called when user clicks a date. Receives ISO start string and allDay flag. */
  onDateClick?: (data: CreateEventInitialData) => void;
  /** Called when user drag-selects a time range. Receives ISO start and end strings. */
  onSelect?: (data: CreateEventInitialData) => void;
  /** Called when user clicks an event. Receives event URI and event data. */
  onEventClick?: (eventUri: string, event: CalendarEvent) => void;
}

// Adapts community.lexicon.calendar.event to FullCalendar event input.
function eventToFullCalendar(record: EventRecord, isInvite: boolean = false): EventInput {
  const { uri, cid, value } = record;
  return {
    id: uri,
    title: value.name,
    start: value.startsAt!,
    end: value.endsAt,
    // Style invites differently: border with transparent background
    backgroundColor: isInvite ? "transparent" : undefined,
    borderColor: isInvite ? "var(--pico-primary-background)" : undefined,
    textColor: isInvite ? "var(--pico-primary-background)" : undefined,
    classNames: isInvite ? ["calendar-invite"] : [],
    extendedProps: {
      description: value.description,
      cid,
      isInvite,
    },
  };
}

function isDisplayable(record: EventRecord): boolean {
  return Boolean(record.value?.name && record.value?.startsAt);
}

export function CalendarView({
  events,
  invites = [],
  userDid,
  emptyComponent,
  onDateClick,
  onSelect,
  onEventClick,
}: CalendarViewProps) {
  const displayableEvents = events.filter(isDisplayable);
  const ownedEventUris = new Set(events.map((e) => e.uri));

  // Use controller functions for data processing
  const eventDataMap = buildEventDataMap(events, invites, userDid);
  const displayableInvites = getDisplayableInvites(invites, ownedEventUris, userDid);

  // Convert events to FullCalendar format
  const fullCalendarEvents: EventInput[] = [
    ...displayableEvents.map((e) => eventToFullCalendar(e, false)),
    ...displayableInvites.map((inv) => {
      const record: EventRecord = {
        uri: inv.invite.subject?.uri || inv.uri,
        cid: inv.cid,
        value: inv.event!,
      };
      return eventToFullCalendar(record, true);
    }),
  ];

  if (fullCalendarEvents.length === 0 && emptyComponent) {
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

  function handleEventClick(info: EventClickArg) {
    const eventUri = info.event.id;
    const eventData = eventDataMap.get(eventUri);
    if (eventData && onEventClick) {
      onEventClick(eventUri, eventData);
    }
  }

  return (
    <FullCalendar
      plugins={[interactionPlugin, dayGridPlugin]}
      initialView="dayGridMonth"
      events={fullCalendarEvents}
      selectable={Boolean(onSelect)}
      dateClick={onDateClick ? handleDateClick : undefined}
      select={onSelect ? handleSelect : undefined}
      eventClick={onEventClick ? handleEventClick : undefined}
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
