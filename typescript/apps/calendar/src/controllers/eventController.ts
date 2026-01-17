import type {
  HabitatClient,
  CreateNotificationInput,
  ListedNotification,
  NotificationRecord,
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
 * EventController handles all Habitat API interactions for calendar events.
 * It abstracts away the complexity of working with the Habitat client,
 * including multi-step operations like creating events with notifications.
 */
export class EventController {
  private client: HabitatClient;
  private userDid: string;

  constructor(client: HabitatClient, userDid: string) {
    this.client = client;
    this.userDid = userDid;
  }

  /**
   * Creates a new calendar event and sends notifications to all invited DIDs.
   * 
   * @param event - The event data (without createdAt, which is auto-generated)
   * @param invitedDids - List of DIDs for people to invite to this event
   * @returns The created event record response
   */
  async createEvent(
    event: Omit<CalendarEvent, "createdAt">,
    invitedDids: string[] = [],
  ): Promise<PutPrivateRecordResponse> {
    const rkey = crypto.randomUUID();
    const eventRecord = {
      ...event,
      createdAt: new Date().toISOString(),
    } as CalendarEvent;

    // Create the event private record
    const eventResponse = await this.client.putPrivateRecord<CalendarEvent>(
      EVENT_COLLECTION,
      eventRecord,
      rkey,
    );

    // Create notifications for each invited DID
    await Promise.all(
      invitedDids.map((did) => this.createEventNotification(did, rkey)),
    );

    return eventResponse;
  }

  /**
   * Creates a notification for an event invitation targeting a specific DID.
   */
  private async createEventNotification(
    targetDid: string,
    eventRkey: string,
  ): Promise<void> {
    const notification: CreateNotificationInput = {
      did: targetDid,
      originDid: this.userDid,
      collection: EVENT_COLLECTION,
      rkey: eventRkey,
    };

    await this.client.createNotification(notification);
  }

  /**
   * Queries for RSVP notifications and creates RSVP records for any that don't exist.
   * 
   * This method:
   * 1. Lists all notifications for the RSVP collection
   * 2. For each notification, checks if a corresponding RSVP exists
   * 3. If no RSVP exists, creates one with a default "pending" state
   * 
   * @returns The list of notifications that were processed
   */
  async getRsvpNotifications(): Promise<NotificationRecord[]> {
    // Query for RSVP notifications
    const notificationsResponse = await this.client.listNotifications(RSVP_COLLECTION);
    const notifications = notificationsResponse.records;

    if (notifications.length === 0) {
      return [];
    }

    // Check each notification and create RSVPs if they don't exist
    await Promise.all(
      notifications.map(async (notification) => {
        const rsvpExists = await this.checkRsvpExists(notification.value.rkey);
        if (!rsvpExists) {
          await this.createRsvpFromNotification(notification.value);
        }
      }),
    );

    return notifications;
  }

  /**
   * Checks if an RSVP record exists for the given rkey.
   */
  private async checkRsvpExists(rkey: string): Promise<boolean> {
    try {
      await this.client.getPrivateRecord<Rsvp>(RSVP_COLLECTION, rkey);
      return true;
    } catch {
      // Record not found
      return false;
    }
  }

  /**
   * Creates an RSVP record from a notification.
   * The RSVP uses the same rkey as referenced in the notification.
   */
  private async createRsvpFromNotification(
    notification: ListedNotification,
  ): Promise<PutPrivateRecordResponse> {
    // Build the event URI from the notification
    const eventUri = `at://${notification.originDid}/${EVENT_COLLECTION}/${notification.rkey}`;
    
    const rsvp: Rsvp = {
      $type: "community.lexicon.calendar.rsvp",
      subject: {
        uri: eventUri,
        cid: "", // CID will be filled in when we fetch the actual event
      },
      status: CommunityLexiconCalendarRsvp.INTERESTED, // Default to interested
    };

    return this.client.putPrivateRecord<Rsvp>(
      RSVP_COLLECTION,
      rsvp,
      notification.rkey,
    );
  }

  /**
   * Lists all events from the user's private records.
   */
  async listEvents() {
    return this.client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION);
  }

  /**
   * Parses an AT URI to extract the DID, collection, and rkey.
   * Format: at://did:plc:xxx/collection.name/rkey
   */
  private parseAtUri(uri: string): { did: string; collection: string; rkey: string } | null {
    const match = uri.match(/^at:\/\/([^/]+)\/([^/]+)\/([^/]+)$/);
    if (!match) return null;
    return { did: match[1], collection: match[2], rkey: match[3] };
  }

  /**
   * Lists all RSVPs with their corresponding event info.
   * Fetches each event from the event owner's repo.
   * Records with invalid format are logged and discarded.
   */
  async listRsvps(): Promise<RsvpWithEvent[]> {
    const rsvpsResponse = await this.client.listPrivateRecords<Rsvp>(RSVP_COLLECTION);

    const rsvpsWithEvents: RsvpWithEvent[] = [];
    console.log("rsvpsResponse", rsvpsResponse);

    for (const record of rsvpsResponse.records) {
      // Validate RSVP has expected schema
      if (!record.value?.subject?.uri) {
        console.error("Invalid RSVP format, missing subject.uri:", record.uri, record.value);
        continue;
      }

      const parsed = this.parseAtUri(record.value.subject.uri);
      if (!parsed) {
        console.error("Invalid subject URI in RSVP:", record.uri, record.value.subject.uri);
        continue;
      }

      let event: CalendarEvent | null = null;
      try {
        console.log("getting event:::", parsed.collection, parsed.rkey, parsed.did);
        const eventResponse = await this.client.getPrivateRecord<CalendarEvent>(
          parsed.collection,
          parsed.rkey,
          parsed.did,
        );
        console.log("eventResponse", eventResponse);
        event = eventResponse.value;
        console.log("event", event);
      } catch {
        // Event not found or not accessible
        event = null;
      }

      rsvpsWithEvents.push({
        uri: record.uri,
        cid: record.cid,
        rsvp: record.value,
        event,
      });
    }
    console.log("rsvpsWithEvents", rsvpsWithEvents);

    return rsvpsWithEvents;
  }

}
