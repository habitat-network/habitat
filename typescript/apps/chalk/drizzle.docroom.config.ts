import { defineConfig } from "drizzle-kit";

// A DocRoom's SQLite lives inside the Durable Object, not in D1, so it is a
// separate database with its own schema and migration history — hence a
// second drizzle config alongside drizzle.config.ts (which owns the D1 docs
// index).
//
// `driver: "durable-sqlite"` makes drizzle-kit emit a `migrations.js`
// barrel next to the .sql files that inlines each migration as a string. A
// Durable Object has no filesystem to read migrations from at runtime, so
// docRoom.ts imports that barrel and hands it to
// `drizzle-orm/durable-sqlite/migrator`'s `migrate()`.
export default defineConfig({
  schema: "./src/server/rooms/docRoomSchema.ts",
  out: "./src/server/rooms/migrations",
  dialect: "sqlite",
  driver: "durable-sqlite",
});
