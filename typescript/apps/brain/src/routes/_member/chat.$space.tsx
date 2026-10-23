import { createFileRoute } from "@tanstack/react-router";
import { Chat } from "@/components/Chat";
import { getChatMessages } from "@/server/functions";

export const Route = createFileRoute("/_member/chat/$space")({
  loader: async ({ params }) => ({
    messages: await getChatMessages({ data: { space: params.space } }),
  }),
  component() {
    const { space } = Route.useParams();
    const { messages } = Route.useLoaderData();
    // Keyed by chat so switching chats starts a fresh useChat.
    return <Chat key={space} space={space} initialMessages={messages} />;
  },
});
