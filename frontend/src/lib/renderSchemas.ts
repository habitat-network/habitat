import type { NetworkHabitatRenderSchema } from "api";

// linkTemplate is a frontend-only extension — not part of the published lexicon.
// Supports {did}, {nsid}, and {rkey} placeholders substituted from the record's AT URI.
export type RenderSchema = NetworkHabitatRenderSchema.Main & {
  linkTemplate?: string;
};
export type FieldSchema = NetworkHabitatRenderSchema.FieldSchema;

const T = "network.habitat.render.schema";

// TODO: make this less hard coded ?
const docsBasePath =
  __HABITAT_DOMAIN__ === "habitat-953995456319.us-west1.run.app"
    ? "habitat.network/habitat/docs/#"
    : __DOMAIN__.replace("frontend", "docs");

export const RENDER_SCHEMA_REGISTRY: Record<string, RenderSchema> = {
  "community.lexicon.calendar.event": {
    $type: `${T}`,
    targetLexicon: "community.lexicon.calendar.event",
    title: "Calendar Event",
    fields: [
      {
        path: "name",
        label: "Name",
        displayType: `${T}#text`,
        priority: `${T}#primary`,
        optional: false,
      },
      {
        path: "description",
        label: "Description",
        displayType: `${T}#text`,
        priority: `${T}#secondary`,
        optional: true,
      },
      {
        path: "startsAt",
        label: "Starts",
        displayType: `${T}#datetime`,
        priority: `${T}#secondary`,
        optional: true,
      },
      {
        path: "endsAt",
        label: "Ends",
        displayType: `${T}#datetime`,
        priority: `${T}#secondary`,
        optional: true,
      },
      {
        path: "status",
        label: "Status",
        displayType: `${T}#badge`,
        priority: `${T}#secondary`,
        optional: true,
      },
    ],
  },

  "network.habitat.docs": {
    $type: `${T}`,
    targetLexicon: "network.habitat.docs",
    title: "Document",
    linkTemplate: `https://${docsBasePath}/{encodedHabitatUri}`,
    fields: [
      {
        path: "name",
        label: "Name",
        displayType: `${T}#text`,
        priority: `${T}#primary`,
        optional: false,
      },
    ],
  },
};
