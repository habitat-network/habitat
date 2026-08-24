import { DurableObject } from "cloudflare:workers";

// Throwaway Durable Object whose only job is to prove the deployment shape:
// that a custom DO class declared here survives the Nitro/Workers build and is
// reachable both from vitest-pool-workers and from a server function at
// runtime. Task 9 deletes it once the real rooms exist.
export class PingRoom extends DurableObject {
  async bump(): Promise<number> {
    const n = ((await this.ctx.storage.get<number>("n")) ?? 0) + 1;
    await this.ctx.storage.put("n", n);
    return n;
  }
}
