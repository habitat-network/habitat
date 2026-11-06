import { useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchServerSentEvents, useChat } from "@tanstack/ai-react";
import type { UIMessage } from "@tanstack/ai-react";
import {
  Badge,
  Card,
  CardContent,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupTextarea,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Spinner,
} from "internal/components/ui";
import {
  ArrowUpIcon,
  BrainIcon,
  CheckIcon,
  ChevronRightIcon,
  SquareIcon,
  WrenchIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { listModels, type ChatMessage } from "@/server/functions";
import { useOrgStore } from "@/stores/org";

// The connection is created once: useChat treats a new one as a new chat.
const connection = fetchServerSentEvents("/api/chat");

function toUIMessage(m: ChatMessage): UIMessage {
  return {
    id: m.id,
    role: m.role,
    parts: [{ type: "text", content: m.content }],
  };
}

type ToolCallPart = Extract<UIMessage["parts"][number], { type: "tool-call" }>;

function pretty(value: unknown): string {
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

function ToolCallStatus({ state }: { state: ToolCallPart["state"] }) {
  if (state === "error") {
    return (
      <Badge variant="destructive">
        <XIcon /> Failed
      </Badge>
    );
  }
  if (state === "complete") {
    return (
      <Badge variant="secondary">
        <CheckIcon /> Done
      </Badge>
    );
  }
  return (
    <Badge variant="outline">
      <Spinner />
      {state === "input-streaming" || state === "awaiting-input"
        ? "Preparing"
        : "Running"}
    </Badge>
  );
}

// ToolCall is one collapsible card: the tool's name and status up top, its
// arguments and result inside.
function ToolCall({
  part,
  message,
}: {
  part: ToolCallPart;
  message: UIMessage;
}) {
  const result = message.parts.find(
    (p) => p.type === "tool-result" && p.toolCallId === part.id,
  );
  const output =
    part.output ??
    (result?.type === "tool-result" ? result.content : undefined);
  return (
    <Collapsible className="group/tool w-full rounded-xl border bg-muted/30 text-sm">
      <CollapsibleTrigger className="flex w-full items-center gap-2 px-3 py-2 text-left">
        <WrenchIcon className="size-3.5 text-muted-foreground" />
        <span className="flex-1 truncate font-mono text-xs">{part.name}</span>
        <ToolCallStatus state={part.state} />
        <ChevronRightIcon className="size-4 text-muted-foreground transition-transform group-data-[open]/tool:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-2 border-t px-3 py-2">
        <div>
          <p className="mb-1 text-xs font-medium text-muted-foreground">
            Arguments
          </p>
          <pre className="max-h-48 overflow-auto rounded-lg bg-muted p-2 font-mono text-xs">
            {part.arguments ? pretty(part.arguments) : "—"}
          </pre>
        </div>
        {output !== undefined && (
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">
              Result
            </p>
            <pre className="max-h-64 overflow-auto rounded-lg bg-muted p-2 font-mono text-xs whitespace-pre-wrap">
              {pretty(output)}
            </pre>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

function Thinking({ content, active }: { content: string; active: boolean }) {
  return (
    <Collapsible className="group/think w-full text-sm text-muted-foreground">
      <CollapsibleTrigger className="flex items-center gap-2">
        <BrainIcon className="size-3.5" />
        {active ? "Thinking…" : "Thought"}
        <ChevronRightIcon className="size-4 transition-transform group-data-[open]/think:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-1 border-l pl-3 whitespace-pre-wrap">
        {content}
      </CollapsibleContent>
    </Collapsible>
  );
}

// Chat is one chat session: its stored messages, then whatever is said in
// this visit. The chat id is the thread id; the server stores each message under it.
export function Chat({
  chatId,
  initialMessages,
}: {
  chatId: string;
  initialMessages: ChatMessage[];
}) {
  const queryClient = useQueryClient();
  const org = useOrgStore((s) => s.selectedOrg);
  const selectedModel = useOrgStore((s) => s.selectedModel);
  const selectModel = useOrgStore((s) => s.selectModel);
  const { data: models = [] } = useQuery({
    queryKey: ["models", org],
    queryFn: () => listModels({ data: { org: org! } }),
    enabled: !!org,
  });
  // A stored pick the org no longer offers falls back to its first model,
  // the same fallback the server applies.
  const model =
    selectedModel && models.includes(selectedModel)
      ? selectedModel
      : (models[0] ?? null);
  const [input, setInput] = useState("");
  const { messages, sendMessage, isLoading, error, stop } = useChat({
    connection,
    threadId: chatId,
    initialMessages: initialMessages.map(toUIMessage),
    onFinish: () => queryClient.invalidateQueries({ queryKey: ["chats"] }),
  });

  const submit = () => {
    const text = input.trim();
    if (!text || isLoading) return;
    // The org and model ride along as forwarded props; the route checks them.
    sendMessage(text, { body: { org, model } });
    setInput("");
  };

  // Keep the newest message in view while it streams.
  const bottomRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [messages, isLoading]);

  const last = messages.at(-1);
  // Nothing visible has streamed in for this turn yet.
  const waiting =
    isLoading &&
    (last?.role !== "assistant" ||
      !last.parts.some((p) => p.type === "text" && p.content)) &&
    // The thinking part shows its own status while it streams.
    last?.parts.at(-1)?.type !== "thinking";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4">
          {messages.map((message, mi) =>
            message.role === "user" ? (
              <Card key={message.id} size="sm" className="max-w-[85%] self-end">
                <CardContent>
                  {message.parts.map((part, i) =>
                    part.type === "text" ? (
                      <p key={i} className="whitespace-pre-wrap">
                        {part.content}
                      </p>
                    ) : null,
                  )}
                </CardContent>
              </Card>
            ) : (
              <div key={message.id} className="flex flex-col gap-2">
                {message.parts.map((part, i) => {
                  if (part.type === "text") {
                    return (
                      <p key={i} className="whitespace-pre-wrap">
                        {part.content}
                      </p>
                    );
                  }
                  if (part.type === "tool-call") {
                    return (
                      <ToolCall key={part.id} part={part} message={message} />
                    );
                  }
                  if (part.type === "thinking" && part.content) {
                    return (
                      <Thinking
                        key={i}
                        content={part.content}
                        active={
                          isLoading &&
                          mi === messages.length - 1 &&
                          i === message.parts.length - 1
                        }
                      />
                    );
                  }
                  return null;
                })}
              </div>
            ),
          )}
          {waiting && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Spinner /> Thinking…
            </div>
          )}
          {error && <p className="text-destructive">{error.message}</p>}
          <div ref={bottomRef} />
        </div>
      </div>
      <div className="mx-auto w-full max-w-3xl shrink-0 bg-background p-4 pt-2">
        <InputGroup>
          <InputGroupTextarea
            value={input}
            placeholder="Ask about your organization's data…"
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <InputGroupAddon align="block-end">
            {models.length > 0 && (
              <Select
                value={model}
                onValueChange={(value) => selectModel(value)}
              >
                <SelectTrigger size="sm" aria-label="Model">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {models.map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {isLoading ? (
              <InputGroupButton
                variant="default"
                size="icon-sm"
                aria-label="Stop"
                onClick={stop}
              >
                <SquareIcon />
              </InputGroupButton>
            ) : (
              <InputGroupButton
                variant="default"
                size="icon-sm"
                aria-label="Send"
                disabled={!input.trim()}
                onClick={submit}
              >
                <ArrowUpIcon />
              </InputGroupButton>
            )}
          </InputGroupAddon>
        </InputGroup>
      </div>
    </div>
  );
}
