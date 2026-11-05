import { useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchServerSentEvents, useChat } from "@tanstack/ai-react";
import type { UIMessage } from "@tanstack/ai-react";
import {
  Badge,
  Card,
  CardContent,
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
import { ArrowUpIcon, SquareIcon } from "lucide-react";
import { useState } from "react";
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

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-4 p-4">
      <div className="flex flex-1 flex-col gap-4">
        {messages.map((message) => (
          <Card
            key={message.id}
            size="sm"
            className={message.role === "user" ? "self-end" : "self-start"}
          >
            <CardContent className="flex flex-col gap-2">
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
                    <Badge key={part.id} variant="secondary">
                      {part.name}
                    </Badge>
                  );
                }
                return null;
              })}
            </CardContent>
          </Card>
        ))}
        {isLoading && <Spinner />}
        {error && <p className="text-destructive">{error.message}</p>}
      </div>
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
            <Select value={model} onValueChange={(value) => selectModel(value)}>
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
  );
}
