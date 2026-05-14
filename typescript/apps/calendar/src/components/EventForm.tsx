import { useState } from "react";
import { useForm } from "react-hook-form";

import { Field, FieldContent, FieldLabel, Input } from "internal/components/ui";
import { UserCombobox } from "internal";

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
  error?: Error | null;
  title?: string;
}

export function EventForm({
  initialEvent,
  onSubmit,
  error,
  title,
}: EventFormProps) {
  const [invitedDids] = useState<string[]>([]);

  const isPending = false;
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
      <Field>
        <FieldLabel>Name</FieldLabel>
        <FieldContent>
          <Input type="text" {...register("name", { required: true })} />
        </FieldContent>
      </Field>
      <Field>
        <FieldLabel>Description</FieldLabel>
        <FieldContent>
          <Input type="text" {...register("description")} />
        </FieldContent>
      </Field>
      <Field>
        <FieldLabel>Starts at</FieldLabel>
        <FieldContent>
          <Input type="datetime-local" {...register("startsAt")} />
        </FieldContent>
      </Field>
      <Field>
        <FieldLabel>Ends at</FieldLabel>
        <FieldContent>
          <Input type="datetime-local" {...register("endsAt")} />
        </FieldContent>
      </Field>
      <Field>
        <FieldLabel>Invite</FieldLabel>
        <FieldContent>
          <UserCombobox onValueChange={() => {}} />
        </FieldContent>
      </Field>
      <div style={{ display: "flex", gap: "0.5rem", marginTop: "1rem" }}>
        <button type="submit" disabled={isPending}>
          {isPending ? "Saving..." : (title ?? "Create Event")}
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
