import { EventForm, type CreateEventInput } from "./EventForm.tsx";

type InitialEvent = Partial<CreateEventInput>;

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
  function handleCancel() {
    onCancel();
    onClose();
  }

  function handleClose() {
    onClose();
  }

  return (
    <dialog
      open={isOpen}
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
