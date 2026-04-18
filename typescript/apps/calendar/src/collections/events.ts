import { createCollection } from "@tanstack/react-db";
import { queryCollectionOptions } from "@tanstack/query-db-collection";
import { AuthManager, listPrivateRecords, TypedRecord } from "internal";
import { CommunityLexiconCalendarEvent } from "api";
import { QueryClient } from "@tanstack/react-query";

export const eventCollection = (
  authManager: AuthManager,
  queryClient: QueryClient,
) =>
  createCollection(
    queryCollectionOptions({
      queryClient,
      queryKey: ["events"],
      getKey: (record: TypedRecord<CommunityLexiconCalendarEvent.Record>) =>
        record.uri,
      queryFn: async () => {
        const events =
          await listPrivateRecords<CommunityLexiconCalendarEvent.Record>(
            authManager,
            "community.lexicon.calendar.event",
          );
        return events.records;
      },
    }),
  );
