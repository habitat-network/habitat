import { openDB, type DBSchema, type IDBPDatabase } from "idb";
import { AuthManager } from "internal/authManager.js";
import { getPrivatePosts, type PrivatePost } from "./habitatApi";

interface GreenskyDB extends DBSchema {
  "private-posts": {
    key: string;
    value: CachedPost;
    indexes: {
      "by-author": string;
      "by-parent": string;
      "by-root": string;
    };
  };
}

export interface CachedPost extends PrivatePost {
  authorDid: string;
  parentUri?: string;
  rootUri?: string;
  fetchedAt: number;
}

function toCachedPost(post: PrivatePost): CachedPost {
  return {
    ...post,
    authorDid: post.uri.split("/")[2] ?? "",
    parentUri: post.value.reply?.parent?.uri,
    rootUri: post.value.reply?.root?.uri,
    fetchedAt: Date.now(),
  };
}

export class PrivatePostCache {
  private constructor(private db: IDBPDatabase<GreenskyDB>) {}

  static async open(): Promise<PrivatePostCache> {
    const db = await openDB<GreenskyDB>("greensky", 1, {
      upgrade(db) {
        const store = db.createObjectStore("private-posts", { keyPath: "uri" });
        store.createIndex("by-author", "authorDid");
        store.createIndex("by-parent", "parentUri");
        store.createIndex("by-root", "rootUri");
      },
    });
    return new PrivatePostCache(db);
  }

  async upsertMany(posts: PrivatePost[]): Promise<void> {
    const tx = this.db.transaction("private-posts", "readwrite");
    await Promise.all(posts.map((p) => tx.store.put(toCachedPost(p))));
    await tx.done;
  }

  async getByAuthor(did: string): Promise<CachedPost[]> {
    return this.db.getAllFromIndex("private-posts", "by-author", did);
  }

  async getReplies(postUri: string): Promise<CachedPost[]> {
    return this.db.getAllFromIndex("private-posts", "by-parent", postUri);
  }

  async getThread(rootUri: string): Promise<CachedPost[]> {
    return this.db.getAllFromIndex("private-posts", "by-root", rootUri);
  }

  close(): void {
    this.db.close();
  }
}

let cachePromise: Promise<PrivatePostCache> | null = null;

export function getPrivatePostCache(): Promise<PrivatePostCache> {
  if (!cachePromise) {
    cachePromise = PrivatePostCache.open();
  }
  return cachePromise;
}

let refreshed = false;

export async function ensureCacheFresh(
  authManager: AuthManager,
): Promise<void> {
  if (refreshed) return;
  refreshed = true;
  const [cache, posts] = await Promise.all([
    getPrivatePostCache(),
    getPrivatePosts(authManager),
  ]);
  await cache.upsertMany(posts);
}
