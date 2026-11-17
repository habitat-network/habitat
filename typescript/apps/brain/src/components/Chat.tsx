import { useQuery, useQueryClient } from "@tanstack/react-query";
import { streamingMarkdownExtension } from "@tanstack/markdown/extensions/streaming";
import { autolinksExtension } from "@tanstack/markdown/extensions/autolinks";
import { Markdown } from "@tanstack/markdown/react";
import { fetchServerSentEvents, useChat } from "@tanstack/ai-react";
import type { UIMessage } from "@tanstack/ai-react";
import {
  Bubble,
  BubbleContent,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupTextarea,
  Message,
  MessageContent,
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
  Trace,
  TraceCollapsible,
  TraceCollapsibleContent,
  TraceCollapsibleTrigger,
  TraceIcon,
  TraceLine,
  TraceTitle,
} from "internal/components/ui";
import {
  ArrowUpIcon,
  ChevronDownIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  LoaderCircleIcon,
  SquareIcon,
} from "lucide-react";
import { useState } from "react";
import { listModels, type ChatMessage } from "@/server/functions";
import { useOrgStore } from "@/stores/org";

const markdownExtensions = [streamingMarkdownExtension(), autolinksExtension()];

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

// ToolCall is one Trace line: a status icon and the tool's name, expanding to
// its arguments and result. A failed call shows in the error style.
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
  const failed = part.state === "error";
  const done = part.state === "complete";
  const preparing =
    part.state === "input-streaming" || part.state === "awaiting-input";
  const status = failed ? "error" : done ? undefined : "running";
  const verb = failed
    ? "Failed"
    : done
      ? "Ran"
      : preparing
        ? "Preparing"
        : "Running";
  return (
    <TraceCollapsible>
      <TraceCollapsibleTrigger>
        <TraceLine status={status}>
          <TraceIcon>
            {failed ? (
              <CircleAlertIcon />
            ) : done ? (
              <CircleCheckIcon />
            ) : (
              <LoaderCircleIcon className="animate-spin" />
            )}
          </TraceIcon>
          <TraceTitle>
            {verb} <span className="font-mono text-xs">{part.name}</span>
          </TraceTitle>
        </TraceLine>
      </TraceCollapsibleTrigger>
      <TraceCollapsibleContent className="flex flex-col gap-2">
        <div>
          <p className="mb-1 text-xs font-medium">Arguments</p>
          <pre className="max-h-48 overflow-auto rounded-lg bg-muted p-2 font-mono text-xs">
            {part.arguments ? pretty(part.arguments) : "—"}
          </pre>
        </div>
        {output !== undefined && (
          <div>
            <p className="mb-1 text-xs font-medium">
              {failed ? "Error" : "Result"}
            </p>
            <pre className="max-h-64 overflow-auto rounded-lg bg-muted p-2 font-mono text-xs whitespace-pre-wrap">
              {pretty(output)}
            </pre>
          </div>
        )}
      </TraceCollapsibleContent>
    </TraceCollapsible>
  );
}

// Thinking is one Trace row; it opens itself while the model is reasoning.
function Thinking({ content, active }: { content: string; active: boolean }) {
  const [open, setOpen] = useState<boolean>();
  return (
    <TraceCollapsible open={open ?? active} onOpenChange={setOpen}>
      <TraceCollapsibleTrigger>
        <TraceLine status={active ? "running" : undefined}>
          <TraceTitle>{active ? "Thinking" : "Thought"}</TraceTitle>
        </TraceLine>
      </TraceCollapsibleTrigger>
      <TraceCollapsibleContent className="whitespace-pre-wrap">
        {content}
      </TraceCollapsibleContent>
    </TraceCollapsible>
  );
}

type Part = UIMessage["parts"][number];
type TraceEntry = {
  part: ToolCallPart | Extract<Part, { type: "thinking" }>;
  index: number;
};
type PartGroup =
  | { kind: "text"; index: number; part: Extract<Part, { type: "text" }> }
  | { kind: "trace"; index: number; parts: TraceEntry[] };

// groupParts folds a reply's consecutive tool calls and thinking into one
// Trace, with text between them as its own block.
function groupParts(parts: Part[]): PartGroup[] {
  const groups: PartGroup[] = [];
  parts.forEach((part, index) => {
    if (part.type === "text") {
      groups.push({ kind: "text", index, part });
    } else if (
      part.type === "tool-call" ||
      (part.type === "thinking" && part.content)
    ) {
      const last = groups.at(-1);
      if (last?.kind === "trace") last.parts.push({ part, index });
      else groups.push({ kind: "trace", index, parts: [{ part, index }] });
    }
  });
  return groups;
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

  const last = messages.at(-1);
  // Nothing visible has streamed in for this turn yet.
  const waiting =
    isLoading &&
    (last?.role !== "assistant" ||
      !last.parts.some((p) => p.type === "text" && p.content)) &&
    // The thinking part shows its own status while it streams.
    last?.parts.at(-1)?.type !== "thinking";

  const modelName = model ?? "Model";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <MessageScrollerProvider>
        <MessageScroller className="min-h-0 flex-1">
          <MessageScrollerViewport>
            <MessageScrollerContent
              aria-busy={isLoading}
              className="mx-auto w-full max-w-3xl gap-6 p-4"
            >
              {messages.map((message, mi) =>
                message.role === "user" ? (
                  <MessageScrollerItem
                    key={message.id}
                    messageId={message.id}
                    scrollAnchor
                  >
                    <Message align="end">
                      <MessageContent>
                        <Bubble align="end" variant="muted">
                          <BubbleContent className="whitespace-pre-wrap">
                            {message.parts.map((part) =>
                              part.type === "text" ? part.content : "",
                            )}
                          </BubbleContent>
                        </Bubble>
                      </MessageContent>
                    </Message>
                  </MessageScrollerItem>
                ) : (
                  <MessageScrollerItem key={message.id} messageId={message.id}>
                    <Message>
                      <MessageContent className="gap-3">
                        {groupParts(message.parts).map((group) =>
                          group.kind === "text" ? (
                            <div
                              key={group.index}
                              className="prose max-w-none break-words prose-sm dark:prose-invert prose-pre:bg-muted prose-pre:text-foreground"
                            >
                              <Markdown extensions={markdownExtensions}>
                                {group.part.content}
                              </Markdown>
                            </div>
                          ) : (
                            <Trace key={group.index}>
                              {group.parts.map(({ part, index }) =>
                                part.type === "tool-call" ? (
                                  <ToolCall
                                    key={part.id}
                                    part={part}
                                    message={message}
                                  />
                                ) : (
                                  <Thinking
                                    key={index}
                                    content={part.content}
                                    active={
                                      isLoading &&
                                      mi === messages.length - 1 &&
                                      index === message.parts.length - 1
                                    }
                                  />
                                ),
                              )}
                            </Trace>
                          ),
                        )}
                      </MessageContent>
                    </Message>
                  </MessageScrollerItem>
                ),
              )}
              {waiting && (
                <Trace>
                  <TraceLine status="running">
                    <TraceTitle>Thinking</TraceTitle>
                  </TraceLine>
                </Trace>
              )}
              {error && <p className="text-destructive">{error.message}</p>}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>
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
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <InputGroupButton
                      size="sm"
                      variant="outline"
                      aria-label={`Model: ${modelName}`}
                    />
                  }
                >
                  {modelName}
                  <ChevronDownIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-56">
                  <DropdownMenuRadioGroup
                    value={model ?? ""}
                    onValueChange={(value) => selectModel(value)}
                  >
                    {models.map((m) => (
                      <DropdownMenuRadioItem key={m} value={m} closeOnClick>
                        {m}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {isLoading ? (
              <InputGroupButton
                variant="default"
                size="icon-sm"
                className="ml-auto rounded-full"
                aria-label="Stop"
                onClick={stop}
              >
                <SquareIcon />
              </InputGroupButton>
            ) : (
              <InputGroupButton
                variant="default"
                size="icon-sm"
                className="ml-auto rounded-full"
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
