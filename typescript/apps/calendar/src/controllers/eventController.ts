import type {
  HabitatClient,
  PutPrivateRecordResponse,
} from "internal/habitatClient.ts";
import {
  CommunityLexiconCalendarEvent,
  CommunityLexiconCalendarRsvp,
} from "api";

// Re-export the lexicon types for convenience
export type CalendarEvent = CommunityLexiconCalendarEvent.Record;
export type Rsvp = CommunityLexiconCalendarRsvp.Record;

// StrongRef type used by RSVP.subject (matches com.atproto.repo.strongRef)
export interface StrongRef {
  uri: string;
  cid: string;
}

export interface RsvpWithEvent {
  uri: string;
  cid: string;
  rsvp: Rsvp;
  event: CalendarEvent | null;
}

const EVENT_COLLECTION = "community.lexicon.calendar.event";
const RSVP_COLLECTION = "community.lexicon.calendar.rsvp";

/**
 * Parses an AT URI to extract the DID, collection, and rkey.
 * Format: at://did:plc:xxx/collection.name/rkey
 */
function parseAtUri(
  uri: string,
): { did: string; collection: string; rkey: string } | null {
  const match = uri.match(/^at:\/\/([^/]+)\/([^/]+)\/([^/]+)$/);
  if (!match) return null;
  return { did: match[1], collection: match[2], rkey: match[3] };
}

/**
 * Creates a new calendar event and automatically sends notifications to all invited DIDs.
 * Notifications are created server-side when grantees are provided.
 *
 * @param client - The Habitat client instance
 * @param userDid - The DID of the current user
 * @param event - The event data (without createdAt, which is auto-generated)
 * @param invitedDids - List of DIDs for people to invite to this event
 * @returns The created event record response
 */
export async function createEvent(
  client: HabitatClient,
  userDid: string,
  event: Omit<CalendarEvent, "createdAt">,
  invitedDids: string[] = [],
): Promise<PutPrivateRecordResponse> {
  const rkey = crypto.randomUUID();
  const eventRecord = {
    ...event,
    createdAt: new Date().toISOString(),
  } as CalendarEvent;

  // Create the event private record with grantees
  // The server will automatically create notifications for local grantees
  return client.putPrivateRecord<CalendarEvent>(
    EVENT_COLLECTION,
    eventRecord,
    rkey,
    { dids: invitedDids }, // Pass as grantees to trigger automatic notification creation
  );
}

/**
 * Lists all events the user has access to (from their own repo and shared via notifications).
 *
 * @param client - The Habitat client instance
 */
export async function listEvents(client: HabitatClient) {
  return client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION);
}

/**
 * Lists all RSVPs with their corresponding event info.
 * Fetches all events and RSVPs upfront, then matches them in memory.
 * Records with invalid format are logged and discarded.
 *
 * @param client - The Habitat client instance
 */
export async function listRsvps(
  client: HabitatClient,
): Promise<RsvpWithEvent[]> {
  // Fetch everything we need in parallel - "fetch the world"
  const [rsvpsResponse, eventsResponse] = await Promise.all([
    client.listPrivateRecords<Rsvp>(RSVP_COLLECTION),
    client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION),
  ]);

  // Build event lookup map: "did/collection/rkey" -> event
  const eventMap = new Map<string, CalendarEvent>();
  for (const record of eventsResponse.records) {
    const parsed = parseAtUri(record.uri);
    if (parsed) {
      const key = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
      eventMap.set(key, record.value);
    }
  }

  // Match RSVPs to events
  const rsvpsWithEvents: RsvpWithEvent[] = [];
  for (const record of rsvpsResponse.records) {
    // Validate RSVP has expected schema
    if (!record.value?.subject?.uri) {
      console.error(
        "Invalid RSVP format, missing subject.uri:",
        record.uri,
        record.value,
      );
      continue;
    }

    const parsed = parseAtUri(record.value.subject.uri);
    if (!parsed) {
      console.error(
        "Invalid subject URI in RSVP:",
        record.uri,
        record.value.subject.uri,
      );
      continue;
    }

    // Look up event in our pre-fetched map
    const eventKey = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
    const event = eventMap.get(eventKey) || null;

    rsvpsWithEvents.push({
      uri: record.uri,
      cid: record.cid,
      rsvp: record.value,
      event,
    });
  }

  return rsvpsWithEvents;
}
