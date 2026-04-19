import {
  createCollection,
  parseWhereExpression,
} from "@tanstack/react-db";
import { queryCollectionOptions } from "@tanstack/query-db-collection";
import { AuthManager, listPrivateRecords, TypedRecord } from "internal";
import { CommunityLexiconCalendarEvent } from "api";
import { QueryClient } from "@tanstack/react-query";

export interface CalendarEventItem {
  source: "pear" | "google";
  uri: string;
  title: string;
  start: string;
  end: string;
  color: string;
}

interface GoogleCalendarEvent {
  name: string;
  startsAt?: string;
  endsAt?: string;
}

const CALENDAR_SERVER_PROXY_HEADER =
  "did:web:calendar-server.dwelf-mirzam.ts.net#calendar";

interface Filter {
  start?: string;
  end?: string;
}

export const eventCollection = (
  authManager: AuthManager,
  queryClient: QueryClient,
) =>
  createCollection(
    queryCollectionOptions({
      queryClient,
      queryKey: ["events"],
      getKey: (record: CalendarEventItem) => record.uri,
      queryFn: async ({ meta }) => {
        try {
          const filter = parseWhereExpression<Filter>(
            meta?.loadSubsetOptions?.where,
            {
              handlers: {
                lt: (_, value) => ({ end: value }),
                gt: (_, value) => ({ start: value }),
                and: (...filters) => Object.assign({}, ...filters),
              },
            },
          );
          const events =
            await listPrivateRecords<CommunityLexiconCalendarEvent.Record>(
              authManager,
              "community.lexicon.calendar.event",
            );
          const pearEvents: CalendarEventItem[] = events.records
            .filter(
              (record: TypedRecord<CommunityLexiconCalendarEvent.Record>) =>
                record.value.startsAt && record.value.endsAt,
            )
            .map(
              (record: TypedRecord<CommunityLexiconCalendarEvent.Record>) => ({
                source: "pear" as const,
                uri: record.uri,
                title: record.value.name,
                start: record.value.startsAt!,
                end: record.value.endsAt!,
                color: "#22c55e",
              }),
            );

          const googleEvents = await fetchGoogleEvents(authManager, filter);

          return [...pearEvents, ...googleEvents];
        } catch (e) {
          console.error(e);
          return [];
        }
      },
    }),
  );

async function fetchGoogleEvents(
  authManager: AuthManager,
  filter: Filter | null,
) {
  const headers = new Headers();
  headers.set("atproto-proxy", CALENDAR_SERVER_PROXY_HEADER);
  const params = new URLSearchParams();
  params.set("timeMin", filter?.start || "");
  params.set("timeMax", filter?.end || "");
  const response = await authManager.fetch(
    "/xrpc/network.habitat.calendar.getEvents",
    "GET",
    undefined,
    headers,
  );

  if (response.ok) {
    const data = (await response.json()) as {
      events: GoogleCalendarEvent[];
    };
    return data.events
      .filter((e) => e.startsAt && e.endsAt)
      .map((e) => ({
        source: "google" as const,
        uri: `google-${e.name}`,
        title: e.name,
        start: e.startsAt!,
        end: e.endsAt!,
        color: "#3b82f6",
      }));
  }
  return [];
}
