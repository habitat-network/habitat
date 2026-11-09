import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { Button } from "../components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../components/ui/dialog";
import { Field, FieldError, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";

// McpTokenDialog connects the caller to a token MCP server with their own
// access token, such as a GitHub personal access token. save stores the token
// (see startMcpAuthorization); the dialog closes once it resolves.
export function McpTokenDialog({
  serverName,
  save,
}: {
  serverName: string;
  save: (token: string) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const {
    register,
    handleSubmit,
    reset: resetForm,
    formState: { isValid },
  } = useForm<{ token: string }>({
    mode: "onChange",
    defaultValues: { token: "" },
  });
  const {
    mutate: connect,
    isPending,
    error,
    reset: resetMutation,
  } = useMutation({ mutationFn: save });

  const submit = handleSubmit(({ token }) =>
    connect(token, {
      onSuccess() {
        setOpen(false);
        resetForm();
      },
    }),
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          resetForm();
          resetMutation();
        }
      }}
    >
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        Add token
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Connect to {serverName}</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={submit}>
          <Field>
            <FieldLabel htmlFor="mcp-token">Access token</FieldLabel>
            <Input
              id="mcp-token"
              type="password"
              autoComplete="off"
              autoFocus
              {...register("token", { required: "Enter your token." })}
            />
            <p className="text-xs text-muted-foreground">
              Your own token for this server, such as a personal access token.
              It&apos;s stored encrypted, used only for your requests, and never
              shown again.
            </p>
          </Field>
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={isPending || !isValid}>
              {isPending ? "Connecting…" : "Connect"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
