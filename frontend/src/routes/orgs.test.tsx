import { fireEvent, screen, waitFor } from "@testing-library/react";
import { SpaceRef } from "@atproto/syntax";
import { HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import {
  TEST_DID,
  fakeAuthManager,
  mockHabitatInstance,
  renderApp,
  xrpcHandler,
} from "@/test/render";
import { server } from "@/test/server";

const membersSpace = (did: string) =>
  new SpaceRef(did, "community.opensocial.members", "self").toString();

const ORG = "did:plc:ewvi7nxzyoun6zhxrhs64oiz";

function mockOrgs(orgs: string[]) {
  server.use(
    xrpcHandler("com.atproto.space.listSpaces", () =>
      HttpResponse.json({
        spaces: orgs.map((did) => ({
          uri: membersSpace(did),
        })),
      }),
    ),
    xrpcHandler("community.opensocial.listInvites", () =>
      HttpResponse.json({ invites: [] }),
    ),
  );
}

describe("/orgs", () => {
  it("redirects unauthenticated users to login", async () => {
    mockHabitatInstance();
    const { router } = await renderApp("/orgs", {
      authManager: fakeAuthManager(null),
    });
    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/oauth-login"),
    );
  });

  it("shows an empty state when the user has no orgs", async () => {
    mockHabitatInstance();
    mockOrgs([]);
    await renderApp("/orgs");
    expect(
      await screen.findByText(/You aren.t a member of any organizations yet/),
    ).toBeDefined();
  });

  it("lists the orgs the user belongs to", async () => {
    mockHabitatInstance();
    mockOrgs([ORG]);
    await renderApp("/orgs");
    // No profile mocked, so the card falls back to the org's DID.
    expect((await screen.findAllByText(ORG)).length).toBeGreaterThan(0);
  });

  it("shows pending invites and accepts one", async () => {
    mockHabitatInstance();
    let accepted = false;
    server.use(
      xrpcHandler("com.atproto.space.listSpaces", () =>
        HttpResponse.json({
          spaces: accepted ? [{ uri: membersSpace(ORG) }] : [],
        }),
      ),
      xrpcHandler("community.opensocial.listInvites", () =>
        HttpResponse.json({
          invites: accepted
            ? []
            : [
                {
                  id: "1",
                  org: ORG,
                  invitee: "did:plc:alice",
                  roles: ["member"],
                  createdAt: "2026-01-01T00:00:00Z",
                },
              ],
        }),
      ),
      xrpcHandler(
        "community.opensocial.requestJoin",
        () => HttpResponse.json({ roles: ["member"] }),
        "post",
      ),
      xrpcHandler(
        "com.atproto.space.putRecord",
        () => {
          accepted = true;
          return HttpResponse.json({
            uri: `at://${TEST_DID}/community.opensocial.acceptance/self`,
            cid: "bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgzuwdz7pm",
          });
        },
        "post",
      ),
    );
    await renderApp("/orgs");
    expect(await screen.findByText("Pending invites (1)")).toBeDefined();
    expect(screen.getByText("member")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Accept" }));

    await waitFor(() =>
      expect(screen.queryByText(/Pending invites/)).toBeNull(),
    );
    expect(accepted).toBe(true);
  });
});

describe("create organization dialog", () => {
  async function openDialog() {
    mockHabitatInstance();
    mockOrgs([]);
    const app = await renderApp("/orgs");
    fireEvent.click(
      await screen.findByRole("button", { name: "New community" }),
    );
    return app;
  }

  it("disables submit until a handle is entered", async () => {
    await openDialog();
    const submit = await screen.findByRole("button", {
      name: "Create organization",
    });
    expect(submit.hasAttribute("disabled")).toBe(true);
    fireEvent.change(screen.getByLabelText("Handle"), {
      target: { value: "acmecorp" },
    });
    expect(submit.hasAttribute("disabled")).toBe(false);
  });

  it("shows the server's error when creation fails", async () => {
    server.use(
      xrpcHandler(
        "network.habitat.opensocial.createOrg",
        () =>
          HttpResponse.json(
            { error: "HandleTaken", message: "handle already taken" },
            { status: 400 },
          ),
        "post",
      ),
    );
    await openDialog();
    fireEvent.change(await screen.findByLabelText("Handle"), {
      target: { value: "acmecorp" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );
    expect(await screen.findByText("handle already taken")).toBeDefined();
  });

  it("creates the org and opens its settings", async () => {
    let createBody: unknown;
    server.use(
      xrpcHandler(
        "network.habitat.opensocial.createOrg",
        async ({ request }) => {
          createBody = await request.json();
          return HttpResponse.json({ org: ORG });
        },
        "post",
      ),
      xrpcHandler(
        "community.opensocial.requestJoin",
        () => HttpResponse.json({ roles: ["admin"] }),
        "post",
      ),
      xrpcHandler(
        "com.atproto.space.putRecord",
        () =>
          HttpResponse.json({
            uri: `at://${TEST_DID}/community.opensocial.acceptance/self`,
            cid: "bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgzuwdz7pm",
          }),
        "post",
      ),
    );
    const { router } = await openDialog();
    fireEvent.change(await screen.findByLabelText("Handle"), {
      target: { value: "acmecorp" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );

    await waitFor(() =>
      expect(router.state.location.pathname).toBe(
        `/orgs/${encodeURIComponent(ORG)}/settings`,
      ),
    );
    expect(createBody).toEqual({ handle: "acmecorp" });
  });
});
