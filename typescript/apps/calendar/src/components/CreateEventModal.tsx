import type { CommunityLexiconCalendarEvent } from "api";
import { useEffect, useRef } from "react";
import { EventForm, type CreateEventInput } from "./EventForm.tsx";

type InitialEvent = Partial<
  Omit<CommunityLexiconCalendarEvent.Record, "createdAt">
>;

interface CreateEventModalProps {
  isOpen: boolean;
  initialEvent?: InitialEvent;
  onClose: () => void;
  onSubmit: (event: CreateEventInput, invitedDids: string[]) => void;
  onCancel: () => void;
  isPending?: boolean;
  error?: Error | null;
}

export function CreateEventModal({
  isOpen,
  initialEvent,
  onClose,
  onSubmit,
  onCancel,
  isPending = false,
  error,
}: CreateEventModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    if (isOpen) {
      dialogRef.current?.showModal();
    } else {
      dialogRef.current?.close();
    }
  }, [isOpen]);

  function handleCancel() {
    onCancel();
    onClose();
  }

  function handleClose() {
    onClose();
  }

  return (
    <dialog
      ref={dialogRef}
      onClose={handleClose}
      onCancel={handleClose}
      style={{ maxWidth: "32rem", overflow: "visible" }}
    >
      <article style={{ overflow: "visible" }}>
        <header>
          <button
            type="button"
            aria-label="Close"
            className="close"
            onClick={handleCancel}
          />
          <h2>Create Event</h2>
        </header>
        <EventForm
          key={initialEvent?.startsAt ?? "new"}
          initialEvent={initialEvent}
          onSubmit={onSubmit}
          onCancel={handleCancel}
          isPending={isPending}
          error={error}
        />
      </article>
    </dialog>
  );
}
