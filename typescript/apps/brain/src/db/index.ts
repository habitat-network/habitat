import { drizzle } from "drizzle-orm/d1";
import { and, asc, desc, eq } from "drizzle-orm";
import { chats, messages, oauthSessions, oauthStates } from "./schema";

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, {
    schema: { oauthStates, oauthSessions, chats, messages },
  });
}

export type Db = ReturnType<typeof getDb>;

export interface ChatSummary {
  id: string;
  title: string;
  createdAt: number;
}

// chatsForOwner lists a member's chats in one org, newest first.
export async function chatsForOwner(
  db: Db,
  ownerDid: string,
  orgDid: string,
): Promise<ChatSummary[]> {
  return db
    .select({ id: chats.id, title: chats.title, createdAt: chats.createdAt })
    .from(chats)
    .where(and(eq(chats.ownerDid, ownerDid), eq(chats.orgDid, orgDid)))
    .orderBy(desc(chats.createdAt), desc(chats.id));
}

// chatOwner returns who owns a chat, or undefined if it doesn't exist yet.
export async function chatOwner(
  db: Db,
  chatId: string,
): Promise<string | undefined> {
  const [row] = await db
    .select({ ownerDid: chats.ownerDid })
    .from(chats)
    .where(eq(chats.id, chatId));
  return row?.ownerDid;
}

// ensureChat creates the chat if it doesn't exist (named by title) and
// reports whether ownerDid owns it. The chat id comes from the browser, so an
// existing chat owned by someone else is left alone and reported as not owned.
export async function ensureChat(
  db: Db,
  chatId: string,
  ownerDid: string,
  orgDid: string,
  title: string,
): Promise<boolean> {
  await db
    .insert(chats)
    .values({ id: chatId, ownerDid, orgDid, title, createdAt: Date.now() })
    .onConflictDoNothing();
  return (await chatOwner(db, chatId)) === ownerDid;
}

export interface StoredMessage {
  id: string;
  role: string;
  content: string;
}

// messagesForChat lists a chat's messages, oldest first.
export async function messagesForChat(
  db: Db,
  chatId: string,
): Promise<StoredMessage[]> {
  return db
    .select({
      id: messages.id,
      role: messages.role,
      content: messages.content,
    })
    .from(messages)
    .where(eq(messages.chatId, chatId))
    .orderBy(asc(messages.createdAt), asc(messages.id));
}

// storeMessage saves one message of a chat. Storing the same id again
// overwrites it, so a resent message isn't repeated.
export async function storeMessage(
  db: Db,
  chatId: string,
  message: StoredMessage,
): Promise<void> {
  await db
    .insert(messages)
    .values({ chatId, ...message, createdAt: Date.now() })
    .onConflictDoUpdate({
      target: [messages.chatId, messages.id],
      set: { role: message.role, content: message.content },
    });
}
