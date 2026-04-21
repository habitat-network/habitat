import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldContent,
  FieldGroup,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { type CreateEventInput } from "./EventForm.tsx";
import { ReactElement } from "react";
import { Controller, useForm } from "react-hook-form";
import { Actor, UserCombobox } from "internal";

interface EventFormFields {
  name: string;
  description: string;
  startsAt: string;
  endsAt: string;
  invitees: Actor[];
}

interface CreateEventModalProps {
  initialEvent?: Partial<CreateEventInput>;
  isOpen?: boolean;
  onClose?: () => void;
  onSubmit?: (event: CreateEventInput, invitedDids: string[]) => void;
  onCancel?: () => void;
  isPending?: boolean;
  error?: Error | null;
  title?: string;
  trigger?: ReactElement;
}

export function CreateEventModal({
  initialEvent,
  title,
  trigger,
  isOpen,
  onClose,
  onSubmit,
}: CreateEventModalProps) {
  const { register, handleSubmit, control } = useForm<EventFormFields>({
    defaultValues: {
      name: initialEvent?.name ?? "",
      description: initialEvent?.description ?? "",
      startsAt: initialEvent?.startsAt
        ? toDatetimeLocal(initialEvent.startsAt)
        : "",
      endsAt: initialEvent?.endsAt ? toDatetimeLocal(initialEvent.endsAt) : "",
    },
  });

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      onClose?.();
    }
  };

  const handle = Dialog.createHandle();
  const handleFormSubmit = async (data: EventFormFields) => {
    if (onSubmit) {
      onSubmit(
        {
          name: data.name,
          description: data.description,
          startsAt: new Date(data.startsAt).toISOString(),
          endsAt: data.endsAt ? new Date(data.endsAt).toISOString() : undefined,
        },
        data.invitees.map((a) => a.did),
      );
      handle.close();
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange} handle={handle}>
      {trigger && <DialogTrigger render={trigger}></DialogTrigger>}
      <DialogContent>
        <DialogTitle>{title ?? "Create Event"}</DialogTitle>

        <form onSubmit={handleSubmit(handleFormSubmit)}>
          <FieldGroup>
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
                <Controller
                  control={control}
                  name="invitees"
                  render={({ field }) => {
                    return (
                      <UserCombobox
                        value={field.value}
                        onValueChange={field.onChange}
                      />
                    );
                  }}
                />
              </FieldContent>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <DialogClose render={<Button variant="secondary" />}>
              Cancel
            </DialogClose>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Converts ISO string to datetime-local input format (YYYY-MM-DDTHH:mm). */
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
