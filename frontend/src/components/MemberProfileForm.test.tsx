import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AuthManager } from "internal";
import { server } from "@/test/msw";
import { myMemberProfileQueryOptions } from "@/queries/opensocial";
import { MemberProfileForm } from "./MemberProfileForm";

const ORG = "did:web:org.test";
const ME = "did:plc:me";
const PEAR = "https://pear.test";
const PDS = "https://pds.test";

// pearAgent resolves the habitat service and mints service-auth tokens; none
// of that is under test here, so point it straight at the mock pear host.
vi.mock("@/queries/pearAgent", () => ({
  pearAgent: () => ({
    fetchHandler: (path: string, init: RequestInit) =>
      fetch(`${PEAR}${path}`, init),
  }),
}));

const authManager = {
  getAuthInfo: () => ({ did: ME }),
  fetchHandler: (path: string, init: RequestInit) =>
    fetch(`${PDS}${path}`, init),
} as unknown as AuthManager;

function Editor() {
  const { data } = useQuery(myMemberProfileQueryOptions(ORG, authManager));
  if (!data) return null;
  return (
    <MemberProfileForm
      org={ORG}
      did={ME}
      initial={data}
      authManager={authManager}
    />
  );
}

function renderEditor() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Editor />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe("MemberProfileForm", () => {
  it("loads the profile served by getProfiles and saves edits as a memberProfile record", async () => {
    server.use(
      http.get(
        `${PEAR}/xrpc/network.habitat.opensocial.getProfiles`,
        ({ request }) => {
          const url = new URL(request.url);
          expect(url.searchParams.get("org")).toBe(ORG);
          expect(url.searchParams.getAll("dids")).toEqual([ME]);
          return HttpResponse.json({
            profiles: [
              { did: ME, handle: "me.test", displayName: "Me", bio: "hello" },
            ],
          });
        },
      ),
    );
    let saved: unknown;
    server.use(
      http.post(
        `${PDS}/xrpc/com.atproto.space.putRecord`,
        async ({ request }) => {
          saved = await request.json();
          return HttpResponse.json({
            uri: `at://${ME}/community.opensocial.memberProfile/${ME}`,
            cid: "bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgz7soitae",
          });
        },
      ),
    );

    renderEditor();
    const name = await screen.findByLabelText("Display name");
    expect((name as HTMLInputElement).value).toBe("Me");
    expect((screen.getByLabelText("Bio") as HTMLTextAreaElement).value).toBe(
      "hello",
    );

    fireEvent.change(name, { target: { value: "Sasha" } });
    fireEvent.change(screen.getByLabelText("Avatar URL"), {
      target: { value: "https://cdn.test/me.png" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await screen.findByText("Saved.");
    expect(saved).toMatchObject({
      space: `at://${ORG}/space/community.opensocial.members/self`,
      repo: ME,
      collection: "community.opensocial.memberProfile",
      rkey: ME,
      record: {
        $type: "community.opensocial.memberProfile",
        displayName: "Sasha",
        bio: "hello",
        avatarUrl: "https://cdn.test/me.png",
      },
    });
  });

  it("shows an error when saving fails", async () => {
    server.use(
      http.get(`${PEAR}/xrpc/network.habitat.opensocial.getProfiles`, () =>
        HttpResponse.json({ profiles: [] }),
      ),
      http.post(`${PDS}/xrpc/com.atproto.space.putRecord`, () =>
        HttpResponse.json(
          { error: "InvalidRequest", message: "nope" },
          { status: 400 },
        ),
      ),
    );

    renderEditor();
    fireEvent.change(await screen.findByLabelText("Display name"), {
      target: { value: "Sasha" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByText("nope")).toBeDefined());
    expect(screen.queryByText("Saved.")).toBeNull();
  });
});
