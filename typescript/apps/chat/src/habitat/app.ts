import { defineApp } from "@habitat-network/habitat";

// CHANNEL_SPACE_TYPE is the type of the space each channel lives in.
export const CHANNEL_SPACE_TYPE = "network.habitat.chat";
export const CHANNEL = "network.habitat.chat.channel";
export const MESSAGE = "network.habitat.chat.message";

// Chat's Habitat app definition: the whole of what it asks the framework
// to sync and index. Every channel is its own org space; the slice keeps
// each channel's descriptor record and its messages. Who can read a channel
// is not declared here: the framework derives it from the org's
// opensocial records on the same sync stream.
export const chat = defineApp({
  name: "chat",
  spaceTypes: [CHANNEL_SPACE_TYPE],
  collections: {
    [CHANNEL]: {},
    [MESSAGE]: {
      fields: { createdAt: "$.createdAt" },
      // A channel's messages in time order: the one query the UI runs.
      indexes: [["space", "createdAt"]],
    },
  },
});
