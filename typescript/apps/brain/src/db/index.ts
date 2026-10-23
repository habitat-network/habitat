import { drizzle } from "drizzle-orm/d1";
import { asc, desc, eq } from "drizzle-orm";
import {
  chats,
  messages,
  oauthSessions,
  oauthStates,
  syncRepos,
  syncSpaces,
} from "./schema";

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, {
    schema: {
      oauthStates,
      oauthSessions,
      syncSpaces,
      syncRepos,
      chats,
      messages,
    },
  });
}

export type Db = ReturnType<typeof getDb>;

export interface ChatSummary {
  space: string;
  title: string;
  createdAt: number;
}

// chatsForOwner lists a member's synced chats, newest first.
export async function chatsForOwner(
  db: Db,
  ownerDid: string,
): Promise<ChatSummary[]> {
  return db
    .select({
      space: chats.space,
      title: chats.title,
      createdAt: chats.createdAt,
    })
    .from(chats)
    .where(eq(chats.ownerDid, ownerDid))
    .orderBy(desc(chats.createdAt), desc(chats.space));
}

export interface StoredMessage {
  id: string;
  role: string;
  content: string;
}

// messagesForSpace lists a chat's synced messages, oldest first. The id is
// the record key, which is also the id the chat UI knows the message by.
export async function messagesForSpace(
  db: Db,
  space: string,
): Promise<StoredMessage[]> {
  const rows = await db
    .select({
      uri: messages.uri,
      role: messages.role,
      content: messages.content,
    })
    .from(messages)
    .where(eq(messages.space, space))
    .orderBy(asc(messages.createdAt), asc(messages.uri));
  return rows.map((r) => ({
    id: r.uri.slice(r.uri.lastIndexOf("/") + 1),
    role: r.role,
    content: r.content,
  }));
}
