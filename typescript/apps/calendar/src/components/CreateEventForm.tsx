import { useForm } from "react-hook-form";

export interface CreateEventFormData {
  name: string;
  description: string;
  startsAt: string;
  endsAt: string;
  invitedDids: string;
}

interface CreateEventFormProps {
  onSubmit: (data: CreateEventFormData) => void;
  isPending: boolean;
  error?: Error | null;
}

export function CreateEventForm({
  onSubmit,
  isPending,
  error,
}: CreateEventFormProps) {
  const { register, handleSubmit } = useForm<CreateEventFormData>({
    defaultValues: {
      name: "",
      description: "",
      startsAt: "",
      endsAt: "",
      invitedDids: "",
    },
  });

  return (
    <form onSubmit={handleSubmit(onSubmit)} style={{ marginBottom: "1rem" }}>
      <div>
        <label>
          Name:
          <input type="text" {...register("name", { required: true })} />
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
        <label>
          Invite (comma-separated DIDs):
          <input
            type="text"
            {...register("invitedDids")}
            placeholder="did:plc:abc123, did:plc:xyz789"
          />
        </label>
      </div>
      <button type="submit" disabled={isPending}>
        {isPending ? "Creating..." : "Save Event"}
      </button>
      {error && <p style={{ color: "red" }}>Error: {error.message}</p>}
    </form>
  );
}
