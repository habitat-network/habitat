import { useState } from "react";
import { useForm } from "react-hook-form";
import { ActorTypeahead } from "./ActorTypeahead.tsx";

/** Event data for creation. */
export interface CreateEventInput {
  name: string;
  description?: string;
  startsAt?: string;
  endsAt?: string;
  [k: string]: unknown;
}

interface EventFormFields {
  name: string;
  description: string;
  startsAt: string;
  endsAt: string;
}

interface EventFormProps {
  /** Pre-filled values (e.g. from calendar dateClick/select). */
  initialEvent?: Partial<CreateEventInput>;
  onSubmit: (event: CreateEventInput, invitedDids: string[]) => void;
  onCancel: () => void;
  isPending?: boolean;
  error?: Error | null;
}

export function EventForm({
  initialEvent,
  onSubmit,
  onCancel,
  isPending = false,
  error,
}: EventFormProps) {
  const [invitedDids, setInvitedDids] = useState<string[]>([]);

  const { register, handleSubmit } = useForm<EventFormFields>({
    defaultValues: {
      name: initialEvent?.name ?? "",
      description: initialEvent?.description ?? "",
      startsAt: initialEvent?.startsAt
        ? toDatetimeLocal(initialEvent.startsAt)
        : "",
      endsAt: initialEvent?.endsAt ? toDatetimeLocal(initialEvent.endsAt) : "",
    },
  });

  function handleFormSubmit(data: EventFormFields) {
    const event: CreateEventInput = {
      name: data.name,
      description: data.description || undefined,
      startsAt: data.startsAt || undefined,
      endsAt: data.endsAt || undefined,
    };

    onSubmit(event, invitedDids);
  }

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)}>
      <div>
        <label>
          Name:
          <input
            type="text"
            {...register("name", { required: true })}
            required
          />
        </label>
      </div>
      <div>
        <label>
          Description:
          <input type="text" {...register("description")} />
        </label>
      </div>
      <div>
        <label>
          Starts At:
          <input type="datetime-local" {...register("startsAt")} />
        </label>
      </div>
      <div>
        <label>
          Ends At:
          <input type="datetime-local" {...register("endsAt")} />
        </label>
      </div>
      <div>
        <ActorTypeahead
          value={invitedDids}
          onChange={setInvitedDids}
          label="Invite"
          placeholder="Search by handle or name..."
          disabled={isPending}
        />
      </div>
      <div style={{ display: "flex", gap: "0.5rem", marginTop: "1rem" }}>
        <button type="submit" disabled={isPending}>
          {isPending ? "Creating..." : "Create Event"}
        </button>
        <button type="button" onClick={onCancel} disabled={isPending}>
          Cancel
        </button>
      </div>
      {error && (
        <p style={{ color: "var(--pico-del-color)", marginTop: "0.5rem" }}>
          Error: {error.message}
        </p>
      )}
    </form>
  );
}

/** Converts ISO string to datetime-local input format (YYYY-MM-DDTHH:mm). */
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
