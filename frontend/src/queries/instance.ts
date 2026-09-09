import { anonymousAgentFor } from "internal";
import { xrpc } from "@atproto/lex";
import { network } from "api";

export async function describeInstance(
  domain: string,
): Promise<network.habitat.instance.describeInstance.$OutputBody> {
  const response = await xrpc(
    anonymousAgentFor(domain),
    network.habitat.instance.describeInstance.main,
    { params: {} },
  );
  return response.body;
}
