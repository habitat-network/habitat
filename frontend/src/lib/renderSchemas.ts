import type { NetworkHabitatRenderSchema } from "api";

export type RenderSchema = NetworkHabitatRenderSchema.Main;
export type FieldSchema = NetworkHabitatRenderSchema.FieldSchema;

const T = "network.habitat.render.schema";

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
