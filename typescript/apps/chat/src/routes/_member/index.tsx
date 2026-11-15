import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input, toast } from "internal/components/ui";
import { Hash, Plus, SendHorizontal, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { z } from "zod";
import { cn } from "@/lib/cn";
import {
  createChannel,
  deleteMessage,
  getCurrentOrg,
  getVersion,
  listChannels,
  listMessages,
  sendMessage,
  type MessageWithHandle,
} from "@/server/functions";

export const Route = createFileRoute("/_member/")({
  validateSearch: z.object({ channel: z.string().optional() }),
  beforeLoad: async () => {
    // The parent's loader hasn't run yet when this does, so ask for the org
    // directly: no org selected means the picker.
    const currentOrg = await getCurrentOrg();
    if (!currentOrg) throw redirect({ to: "/orgs" });
    return { currentOrg };
  },
  loader: async ({ context }) => ({
    currentOrg: context.currentOrg,
    member: context.member,
    channels: await listChannels(),
  }),
  component: ChatPage,
});

// How often the page asks for the slice's version. Synced rows land in D1
// as space notifications arrive, and the version moves with each one, so a
// changed version is the cue to refetch channels and messages.
const VERSION_POLL_MS = 2_000;

function ChatPage() {
  const {
    currentOrg,
    member,
    channels: initialChannels,
  } = Route.useLoaderData();
  const { channel: selected } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const queryClient = useQueryClient();

  const channelsKey = ["channels", currentOrg.did];
  const { data: channels = [] } = useQuery({
    queryKey: channelsKey,
    queryFn: () => listChannels(),
    initialData: initialChannels,
  });
  // The channel in the URL if the member can (still) read it, else the first.
  const current = channels.find((c) => c.space === selected) ?? channels[0];

  const messagesKey = ["messages", current?.space];
  const { data } = useQuery({
    queryKey: messagesKey,
    queryFn: () => listMessages({ data: { space: current!.space } }),
    enabled: current !== undefined,
  });

  const { data: version } = useQuery({
    queryKey: ["version"],
    queryFn: () => getVersion(),
    refetchInterval: VERSION_POLL_MS,
  });
  useEffect(() => {
    if (version === undefined) return;
    void queryClient.invalidateQueries({ queryKey: ["channels"] });
    void queryClient.invalidateQueries({ queryKey: ["messages"] });
  }, [version, queryClient]);

  return (
    <div className="mx-auto flex h-[calc(100svh-4rem)] w-full max-w-5xl gap-0 px-6 py-6">
      <aside className="flex w-60 shrink-0 flex-col gap-3 rounded-l-2xl border border-border bg-card p-3">
        <NewChannel
          onCreated={(space) => {
            void queryClient.invalidateQueries({ queryKey: channelsKey });
            void navigate({ search: { channel: space } });
          }}
        />
        <nav className="flex flex-col gap-0.5 overflow-y-auto">
          {channels.map((c) => (
            <button
              key={c.space}
              type="button"
              onClick={() => void navigate({ search: { channel: c.space } })}
              className={cn(
                "flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-muted",
                c.space === current?.space && "bg-muted font-medium",
              )}
            >
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{c.name}</span>
            </button>
          ))}
          {channels.length === 0 && (
            <p className="px-2.5 py-4 text-sm text-muted-foreground">
              No channels yet. Create the first one for{" "}
              {currentOrg.name ?? "your organization"}.
            </p>
          )}
        </nav>
      </aside>

      <section className="flex min-w-0 flex-1 flex-col rounded-r-2xl border border-l-0 border-border bg-background">
        {current ? (
          <>
            <header className="flex flex-col gap-0.5 border-b border-border px-5 py-3">
              <h1 className="flex items-center gap-1.5 text-base font-semibold">
                <Hash className="size-4 text-muted-foreground" />
                {current.name}
              </h1>
              {current.topic && (
                <p className="text-sm text-muted-foreground">{current.topic}</p>
              )}
            </header>
            <MessageList
              space={current.space}
              messages={data?.messages ?? []}
              memberDid={member.did}
              onChanged={() =>
                void queryClient.invalidateQueries({ queryKey: messagesKey })
              }
            />
            <Composer
              space={current.space}
              channelName={current.name}
              onSent={() =>
                void queryClient.invalidateQueries({ queryKey: messagesKey })
              }
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            Create a channel to start talking.
          </div>
        )}
      </section>
    </div>
  );
}

function MessageList({
  space,
  messages,
  memberDid,
  onChanged,
}: {
  space: string;
  messages: MessageWithHandle[];
  memberDid: string;
  onChanged: () => void;
}) {
  const end = useRef<HTMLDivElement>(null);
  const last = messages[messages.length - 1]?.uri;
  // Keep the newest message in view when one arrives or the channel changes.
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [last, space]);

  const remove = useMutation({
    mutationFn: (rkey: string) => deleteMessage({ data: { space, rkey } }),
    onSuccess: onChanged,
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't delete the message",
        description: error.message,
      }),
  });

  return (
    <div className="flex flex-1 flex-col gap-3 overflow-y-auto px-5 py-4">
      {messages.length === 0 && (
        <p className="m-auto text-sm text-muted-foreground">No messages yet.</p>
      )}
      {messages.map((m) => (
        <div key={m.uri} className="group flex flex-col gap-0.5">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-medium">
              {m.authorHandle ?? m.author}
            </span>
            <time
              className="text-xs text-muted-foreground"
              dateTime={m.createdAt}
            >
              {formatTime(m.createdAt)}
            </time>
            {m.author === memberDid && (
              <button
                type="button"
                aria-label="Delete message"
                disabled={remove.isPending}
                onClick={() => remove.mutate(m.rkey)}
                className="ml-auto text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
              >
                <Trash2 className="size-3.5" />
              </button>
            )}
          </div>
          <p className="text-sm break-words whitespace-pre-wrap">{m.text}</p>
        </div>
      ))}
      <div ref={end} />
    </div>
  );
}

function Composer({
  space,
  channelName,
  onSent,
}: {
  space: string;
  channelName: string;
  onSent: () => void;
}) {
  const [text, setText] = useState("");
  const send = useMutation({
    mutationFn: (body: string) => sendMessage({ data: { space, text: body } }),
    onSuccess: () => {
      setText("");
      onSent();
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't send the message",
        description: error.message,
      }),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body = text.trim();
    if (body.length > 0 && !send.isPending) send.mutate(body);
  };
  return (
    <form onSubmit={submit} className="flex gap-2 border-t border-border p-3">
      <Input
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={`Message #${channelName}`}
        aria-label={`Message #${channelName}`}
        maxLength={4000}
        autoComplete="off"
      />
      <Button
        type="submit"
        loading={send.isPending}
        disabled={text.trim() === ""}
      >
        <SendHorizontal data-icon="inline-start" />
        Send
      </Button>
    </form>
  );
}

function NewChannel({ onCreated }: { onCreated: (space: string) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [topic, setTopic] = useState("");
  const create = useMutation({
    mutationFn: () => createChannel({ data: { name, topic } }),
    onSuccess: ({ space, syncError }) => {
      setOpen(false);
      setName("");
      setTopic("");
      if (syncError) {
        toast.add({
          type: "error",
          title: "Channel created, but it hasn't synced yet",
          description: syncError,
        });
      }
      onCreated(space);
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't create the channel",
        description: error.message,
      }),
  });
  if (!open) {
    return (
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Plus data-icon="inline-start" />
        New channel
      </Button>
    );
  }
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (name.trim() !== "" && !create.isPending) create.mutate();
      }}
    >
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Channel name"
        aria-label="Channel name"
        maxLength={64}
      />
      <Input
        value={topic}
        onChange={(e) => setTopic(e.target.value)}
        placeholder="Topic (optional)"
        aria-label="Topic"
        maxLength={256}
      />
      <div className="flex gap-2">
        <Button
          type="submit"
          size="sm"
          loading={create.isPending}
          disabled={name.trim() === ""}
        >
          Create
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => setOpen(false)}
        >
          Cancel
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Open to every member of the organization.
      </p>
    </form>
  );
}

function formatTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}
