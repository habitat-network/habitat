import React, { useEffect, useState } from "react";
import type {
  CalendarEvent,
  InviteWithEvent,
  RsvpWithEvent,
  RsvpStatus,
} from "../controllers/eventController.ts";
import { RSVP_STATUS } from "../controllers/eventController.ts";

interface BlueskyProfile {
  did: string;
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface EventDetailsModalProps {
  isOpen: boolean;
  event: CalendarEvent | null;
  eventUri: string | null;
  invites: InviteWithEvent[];
  rsvps: RsvpWithEvent[];
  userDid: string;
  onClose: () => void;
  onRsvp: (eventUri: string, status: RsvpStatus) => void;
  isRsvpPending?: boolean;
}

async function fetchBlueskyProfile(did: string): Promise<BlueskyProfile | null> {
  try {
    const response = await fetch(
      `https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile?actor=${encodeURIComponent(did)}`
    );
    if (!response.ok) return null;
    const data = await response.json();
    return {
      did: data.did,
      handle: data.handle,
      displayName: data.displayName,
      avatar: data.avatar,
    };
  } catch {
    return null;
  }
}

function useBlueskyProfiles(dids: string[]) {
  const [profiles, setProfiles] = useState<Map<string, BlueskyProfile>>(new Map());
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (dids.length === 0) return;

    const uniqueDids = [...new Set(dids)];
    const uncachedDids = uniqueDids.filter((did) => !profiles.has(did));

    if (uncachedDids.length === 0) return;

    setLoading(true);
    Promise.all(uncachedDids.map(fetchBlueskyProfile)).then((results) => {
      setProfiles((prev) => {
        const next = new Map(prev);
        results.forEach((profile) => {
          if (profile) {
            next.set(profile.did, profile);
          }
        });
        return next;
      });
      setLoading(false);
    });
  }, [dids.join(",")]);

  return { profiles, loading };
}

function getDidFromUri(uri: string): string | null {
  const match = uri.match(/^(?:at|habitat):\/\/([^/]+)\//);
  return match ? match[1] : null;
}

function formatDateLine(startsAt?: string, endsAt?: string): string {
  if (!startsAt) return "Date not set";
  const start = new Date(startsAt);
  
  const datePart = start.toLocaleDateString(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
  });
  
  const startTime = start.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });
  
  if (!endsAt) return `${datePart} · ${startTime}`;
  
  const end = new Date(endsAt);
  const endTime = end.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });
  
  return `${datePart} · ${startTime} – ${endTime}`;
}

function getRsvpBadge(status: string | null): React.ReactNode {
  const baseStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    padding: "2px 8px",
    borderRadius: "12px",
    fontSize: "12px",
    fontWeight: 500,
    marginLeft: "8px",
  };

  if (status === RSVP_STATUS.GOING) {
    return (
      <span style={{ ...baseStyle, backgroundColor: "rgba(157, 173, 111, 0.2)", color: "#7a8a56" }}>
        ✓ Yes
      </span>
    );
  }
  if (status === RSVP_STATUS.NOT_GOING) {
    return (
      <span style={{ ...baseStyle, backgroundColor: "rgba(196, 125, 125, 0.2)", color: "#a05a5a" }}>
        ✕ No
      </span>
    );
  }
  if (status === RSVP_STATUS.INTERESTED) {
    return (
      <span style={{ ...baseStyle, backgroundColor: "rgba(212, 184, 106, 0.2)", color: "#9a8540" }}>
        ? Maybe
      </span>
    );
  }
  return (
    <span style={{ ...baseStyle, backgroundColor: "var(--pico-muted-border-color)", color: "var(--pico-muted-color)" }}>
      Awaiting
    </span>
  );
}

const styles = {
  overlay: {
    position: "fixed" as const,
    inset: 0,
    backgroundColor: "rgba(0, 0, 0, 0.3)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 1000,
  },
  card: {
    backgroundColor: "var(--pico-background-color)",
    borderRadius: "var(--pico-border-radius)",
    boxShadow: "0 4px 24px rgba(0, 0, 0, 0.15)",
    width: "400px",
    maxWidth: "90vw",
    maxHeight: "85vh",
    overflow: "hidden",
    display: "flex",
    flexDirection: "column" as const,
  },
  header: {
    display: "flex",
    justifyContent: "flex-end",
    padding: "8px 8px 0",
    gap: "4px",
  },
  iconButton: {
    background: "none",
    border: "none",
    padding: "8px",
    cursor: "pointer",
    borderRadius: "50%",
    color: "var(--pico-muted-color)",
    fontSize: "18px",
  },
  content: {
    padding: "0 24px 16px",
    overflowY: "auto" as const,
    flex: 1,
  },
  title: {
    display: "flex",
    alignItems: "flex-start",
    gap: "12px",
    marginBottom: "4px",
  },
  eventName: {
    fontSize: "22px",
    fontWeight: 400,
    color: "var(--pico-color)",
    margin: 0,
    lineHeight: 1.3,
  },
  dateLine: {
    fontSize: "14px",
    color: "var(--pico-color)",
    marginLeft: "32px",
    marginBottom: "16px",
  },
  section: {
    display: "flex",
    alignItems: "flex-start",
    gap: "16px",
    padding: "12px 0",
    borderTop: "1px solid var(--pico-muted-border-color)",
  },
  sectionIcon: {
    color: "var(--pico-muted-color)",
    fontSize: "20px",
    width: "20px",
    textAlign: "center" as const,
    flexShrink: 0,
  },
  sectionContent: {
    flex: 1,
    minWidth: 0,
  },
  linkButton: {
    display: "inline-flex",
    alignItems: "center",
    gap: "8px",
    padding: "8px 16px",
    backgroundColor: "var(--pico-primary-background)",
    color: "var(--pico-primary-inverse)",
    borderRadius: "var(--pico-border-radius)",
    textDecoration: "none",
    fontSize: "14px",
    fontWeight: 500,
  },
  linkUrl: {
    fontSize: "12px",
    color: "var(--pico-muted-color)",
    marginTop: "4px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap" as const,
  },
  guestSummary: {
    fontSize: "14px",
    fontWeight: 500,
    color: "var(--pico-color)",
  },
  guestSubtext: {
    fontSize: "12px",
    color: "var(--pico-muted-color)",
  },
  guestList: {
    marginTop: "12px",
    display: "flex",
    flexDirection: "column" as const,
    gap: "8px",
  },
  guestRow: {
    display: "flex",
    alignItems: "center",
    gap: "12px",
  },
  avatar: {
    width: "32px",
    height: "32px",
    borderRadius: "50%",
    backgroundColor: "var(--pico-muted-border-color)",
    backgroundSize: "cover",
    backgroundPosition: "center",
    flexShrink: 0,
  },
  guestInfo: {
    flex: 1,
    minWidth: 0,
    display: "flex",
    alignItems: "center",
    flexWrap: "wrap" as const,
    gap: "4px",
  },
  guestName: {
    fontSize: "14px",
    color: "var(--pico-color)",
  },
  guestRole: {
    fontSize: "12px",
    color: "var(--pico-muted-color)",
  },
  footer: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "12px 24px",
    backgroundColor: "var(--pico-card-background-color)",
    borderTop: "1px solid var(--pico-muted-border-color)",
  },
  footerLabel: {
    fontSize: "14px",
    color: "var(--pico-color)",
  },
  rsvpButtons: {
    display: "flex",
    gap: "8px",
  },
  rsvpButton: {
    padding: "8px 16px",
    border: "2px solid var(--pico-primary-background)",
    borderRadius: "var(--pico-border-radius)",
    backgroundColor: "transparent",
    fontSize: "14px",
    cursor: "pointer",
    color: "var(--pico-primary-border)",
    fontWeight: 500,
    transition: "all 0.15s ease",
  },
  rsvpButtonActive: {
    backgroundColor: "var(--pico-primary-background)",
    borderColor: "var(--pico-primary-background)",
    color: "var(--pico-primary-inverse)",
  },
};

export function EventDetailsModal({
  isOpen,
  event,
  eventUri,
  invites,
  rsvps,
  userDid,
  onClose,
  onRsvp,
  isRsvpPending = false,
}: EventDetailsModalProps) {
  if (!isOpen || !event || !eventUri) return null;

  const eventInvites = invites.filter((inv) => inv.invite.subject?.uri === eventUri);
  const eventRsvps = rsvps.filter((rsvp) => rsvp.rsvp.subject?.uri === eventUri);

  // Get event owner DID from the eventUri
  const eventOwnerDid = getDidFromUri(eventUri);
  
  // Collect all DIDs: invitees + owner
  const allDids = [...eventInvites.map((inv) => inv.invite.invitee)];
  if (eventOwnerDid && !allDids.includes(eventOwnerDid)) {
    allDids.unshift(eventOwnerDid);
  }
  
  const { profiles, loading: profilesLoading } = useBlueskyProfiles(allDids);

  const userRsvp = eventRsvps.find((rsvp) => getDidFromUri(rsvp.uri) === userDid);
  const userInvite = eventInvites.find((inv) => inv.invite.invitee === userDid);
  const isOrganizer = eventOwnerDid === userDid;
  const canRsvp = isOrganizer || userInvite;

  // Build guest list with RSVP status
  const guests = allDids.map((did) => {
    const rsvp = eventRsvps.find((r) => getDidFromUri(r.uri) === did);
    const isOrganizer = did === eventOwnerDid;
    return {
      did,
      rsvpStatus: rsvp?.rsvp.status || null,
      isOrganizer,
    };
  });

  // Calculate RSVP summary
  const goingCount = guests.filter((g) => g.rsvpStatus === RSVP_STATUS.GOING).length;
  const awaitingCount = guests.filter((g) => !g.rsvpStatus).length;
  
  let summaryText = "";
  if (goingCount > 0 && awaitingCount > 0) {
    summaryText = `${goingCount} yes, ${awaitingCount} awaiting`;
  } else if (goingCount > 0) {
    summaryText = `${goingCount} yes`;
  } else if (awaitingCount > 0) {
    summaryText = `${awaitingCount} awaiting`;
  }

  // Get first link for prominent display
  const primaryLink = event.uris?.[0];

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.card} onClick={(e) => e.stopPropagation()}>
        {/* Header with close button */}
        <div style={styles.header}>
          <button style={styles.iconButton} onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        {/* Content */}
        <div style={styles.content}>
          {/* Title */}
          <div style={styles.title}>
            <span style={{ fontSize: "20px" }}>📅</span>
            <h2 style={styles.eventName}>{event.name}</h2>
          </div>

          {/* Date/Time */}
          <div style={styles.dateLine}>
            {formatDateLine(event.startsAt, event.endsAt)}
          </div>

          {/* Description */}
          {event.description && (
            <div style={{ ...styles.section, borderTop: "none", paddingTop: 0 }}>
              <span style={styles.sectionIcon}>📝</span>
              <div style={{ ...styles.sectionContent, color: "var(--pico-color)", fontSize: "14px", whiteSpace: "pre-wrap" }}>
                {event.description}
              </div>
            </div>
          )}

          {/* Primary Link */}
          {primaryLink && (
            <div style={styles.section}>
              <span style={styles.sectionIcon}>🔗</span>
              <div style={styles.sectionContent}>
                <a
                  href={primaryLink.uri}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={styles.linkButton}
                >
                  {primaryLink.name || "Open Link"}
                </a>
                <div style={styles.linkUrl}>{primaryLink.uri}</div>
              </div>
            </div>
          )}

          {/* Guests */}
          {guests.length > 0 && (
            <div style={styles.section}>
              <span style={styles.sectionIcon}>👥</span>
              <div style={styles.sectionContent}>
                <div style={styles.guestSummary}>
                  {guests.length} guest{guests.length !== 1 ? "s" : ""}
                </div>
                {summaryText && (
                  <div style={styles.guestSubtext}>{summaryText}</div>
                )}
                <div style={styles.guestList}>
                  {guests.map((guest) => {
                    const profile = profiles.get(guest.did);
                    const isYou = guest.did === userDid;
                    return (
                      <div key={guest.did} style={styles.guestRow}>
                        <div
                          style={{
                            ...styles.avatar,
                            backgroundImage: profile?.avatar
                              ? `url(${profile.avatar})`
                              : undefined,
                          }}
                        />
                        <div style={styles.guestInfo}>
                          <span style={styles.guestName}>
                            {profile?.displayName || profile?.handle || (profilesLoading ? "Loading..." : guest.did.slice(0, 24) + "...")}
                            {isYou && " (you)"}
                          </span>
                          {guest.isOrganizer && (
                            <span style={styles.guestRole}>· Organizer</span>
                          )}
                          {getRsvpBadge(guest.rsvpStatus)}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          )}

          {/* Additional Links */}
          {event.uris && event.uris.length > 1 && (
            <div style={styles.section}>
              <span style={styles.sectionIcon}>🔗</span>
              <div style={styles.sectionContent}>
                {event.uris.slice(1).map((uri, i) => (
                  <div key={i} style={{ marginBottom: "8px" }}>
                    <a
                      href={uri.uri}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{ fontSize: "14px", color: "var(--pico-primary-border)" }}
                    >
                      {uri.name || uri.uri}
                    </a>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Locations */}
          {event.locations && event.locations.length > 0 && (
            <div style={styles.section}>
              <span style={styles.sectionIcon}>📍</span>
              <div style={styles.sectionContent}>
                {event.locations.map((loc, i) => (
                  <div key={i} style={{ fontSize: "14px", color: "var(--pico-color)" }}>
                    {formatLocation(loc)}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Footer with RSVP */}
        {canRsvp && (
          <div style={styles.footer}>
            <span style={styles.footerLabel}>Going?</span>
            <div style={styles.rsvpButtons}>
              <button
                style={{
                  ...styles.rsvpButton,
                  ...(userRsvp?.rsvp.status === RSVP_STATUS.GOING ? styles.rsvpButtonActive : {}),
                }}
                onClick={() => onRsvp(eventUri, RSVP_STATUS.GOING)}
                disabled={isRsvpPending}
              >
                Yes
              </button>
              <button
                style={{
                  ...styles.rsvpButton,
                  ...(userRsvp?.rsvp.status === RSVP_STATUS.NOT_GOING ? styles.rsvpButtonActive : {}),
                }}
                onClick={() => onRsvp(eventUri, RSVP_STATUS.NOT_GOING)}
                disabled={isRsvpPending}
              >
                No
              </button>
              <button
                style={{
                  ...styles.rsvpButton,
                  ...(userRsvp?.rsvp.status === RSVP_STATUS.INTERESTED ? styles.rsvpButtonActive : {}),
                }}
                onClick={() => onRsvp(eventUri, RSVP_STATUS.INTERESTED)}
                disabled={isRsvpPending}
              >
                Maybe
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function formatLocation(loc: unknown): React.ReactNode {
  if (!loc || typeof loc !== "object") return null;
  const l = loc as Record<string, unknown>;

  if ("uri" in l && typeof l.uri === "string") {
    return (
      <a
        href={l.uri}
        target="_blank"
        rel="noopener noreferrer"
        style={{ color: "var(--pico-primary-border)" }}
      >
        {(l.name as string) || l.uri}
      </a>
    );
  }
  if ("streetAddress" in l) {
    const parts = [l.streetAddress, l.locality, l.region, l.postalCode].filter(Boolean);
    return <span>{parts.join(", ")}</span>;
  }
  if ("latitude" in l && "longitude" in l) {
    return <span>{l.latitude}, {l.longitude}</span>;
  }
  return <span>{JSON.stringify(l)}</span>;
}
