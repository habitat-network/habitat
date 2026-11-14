import {
  createRuntime,
  type HabitatAgent,
  type HabitatRuntime,
} from "@habitat-network/habitat";
import { chat } from "@/habitat/app";

// chatRuntime binds Chat's Habitat app to this deployment's D1 database.
// Queries read the slice there as the viewer. Mutations write through the
// agent `agentFor` returns for the viewer (their own OAuth session with the
// Habitat instance, see functions.ts), so pear enforces each channel's
// policy; the syncer, which only uses the runtime's sink, passes none. The
// slice's tables are created by the runtime itself on first use (they
// aren't in the drizzle migrations).
export function chatRuntime(
  env: Env,
  agentFor?: (viewer: string) => Promise<HabitatAgent>,
): HabitatRuntime {
  return createRuntime(chat, { db: env.DB, agent: agentFor });
}
