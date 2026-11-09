import { createFileRoute } from "@tanstack/react-router";
import { Chat } from "@/components/Chat";
import { getChatMessages } from "@/server/functions";

export const Route = createFileRoute("/_member/chat/$chat")({
  loader: async ({ params }) => ({
    messages: await getChatMessages({ data: { chat: params.chat } }),
  }),
  component() {
    const { chat } = Route.useParams();
    const { messages } = Route.useLoaderData();
    // Keyed by chat so switching chats starts a fresh useChat.
    return <Chat key={chat} chatId={chat} initialMessages={messages} />;
  },
});
