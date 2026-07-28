import { query } from "internal";
import type { NetworkHabitatInstanceDescribeInstance } from "api";

export function describeInstance(
  domain: string,
): Promise<NetworkHabitatInstanceDescribeInstance.OutputSchema> {
  return query(
    "network.habitat.instance.describeInstance",
    {},
    { unauthenticated: true, domain },
  );
}
