import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useState } from "react";
import { FRUITS, FRUIT_KEYS, getFruit } from "@/fruits";

interface MemberRecord {
  uri: string;
  did: string;
  displayName: string;
  avatarCid?: string;
  funFact?: string;
  favoriteFruit?: string;
  createdAt: string;
}

async function fetchMembers(): Promise<MemberRecord[]> {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) throw new Error("failed to fetch members");
  return res.json();
}

async function fetchSpaceURI(): Promise<string | null> {
  const res = await fetch(`${__FRUITGANG_API__}/getSpaceURI`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error("fetch space URI failed");
  return ((await res.json()) as { uri: string }).uri;
}

export const Route = createFileRoute("/_app/members")({
  component: MembersPage,
});

function MembersPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();
  const did = authManager.getAuthInfo()?.did ?? "";
  const { data: members = [], isLoading } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const { data: spaceURI } = useQuery({ queryKey: ["spaceURI"], queryFn: fetchSpaceURI, staleTime: 1000 * 60 * 5 });

  const hasMember = members.some((m) => m.did === did);
  const [form, setForm] = useState({ displayName: "", funFact: "", favoriteFruit: "strawberry" });

  const { mutate: createProfile, isPending } = useMutation({
    mutationFn: async () => {
      await procedure("network.habitat.space.putRecord", {
        space: spaceURI ?? undefined,
        collection: "community.fruitgang.member",
        rkey: "self",
        record: {
          displayName: form.displayName,
          funFact: form.funFact,
          favoriteFruit: `community.fruitgang.member#${form.favoriteFruit}`,
          createdAt: new Date().toISOString(),
        },
      }, { authManager });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["members"] }),
  });

  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "2rem", color: "var(--text)", marginBottom: "0.5rem" }}>
        the gang 🍉
      </h2>
      <p style={{ color: "var(--muted)", fontSize: "0.95rem", lineHeight: 1.7, marginBottom: "1.75rem", maxWidth: "560px" }}>
        This is a community for fruit lovers! Whether you're a devoted follower of the <em>Fructus</em> or a sworn
        defender of the humble <em>Bacas</em>, you belong here. We celebrate all fruit — the noble <em>Pomum</em>,
        the wild <em>Frux</em>, and everything in between. Pick your favorite, log your snacks, and chat with
        fellow fruit enthusiasts. No citrus left behind. 🍊🍋🍇
      </p>

      {!hasMember && (
        <div style={{
          background: "var(--surface)",
          border: "1px dashed var(--border)",
          borderRadius: "var(--radius-card)",
          padding: "1.5rem",
          marginBottom: "2rem",
        }}>
          <p style={{ color: "var(--muted)", fontWeight: 600, marginBottom: "1rem" }}>
            you're not in the gang yet — introduce yourself!
          </p>
          <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem", maxWidth: "400px" }}>
            <input
              placeholder="display name"
              value={form.displayName}
              onChange={(e) => setForm((f) => ({ ...f, displayName: e.target.value }))}
              style={inputStyle}
            />
            <input
              placeholder="fun fact"
              value={form.funFact}
              onChange={(e) => setForm((f) => ({ ...f, funFact: e.target.value }))}
              style={inputStyle}
            />
            <select
              value={form.favoriteFruit}
              onChange={(e) => setForm((f) => ({ ...f, favoriteFruit: e.target.value }))}
              style={inputStyle}
            >
              {FRUIT_KEYS.map((key) => (
                <option key={key} value={key}>
                  {FRUITS[key].emoji} {FRUITS[key].label}
                </option>
              ))}
            </select>
            <button
              onClick={() => createProfile()}
              disabled={isPending || !form.displayName}
              style={buttonStyle("var(--strawberry)")}
            >
              {isPending ? "joining…" : "join the gang 🍓"}
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <p style={{ color: "var(--muted)" }}>loading members…</p>
      ) : (
        <div style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))",
          gap: "1rem",
        }}>
          {members.map((m) => <MemberCard key={m.uri} member={m} />)}
        </div>
      )}
    </div>
  );
}

function MemberCard({ member }: { member: MemberRecord }) {
  const fruit = member.favoriteFruit ? getFruit(member.favoriteFruit) : undefined;
  const accentColor = fruit ? `var(${fruit.colorVar})` : "var(--muted)";
  const joinDate = new Date(member.createdAt).toLocaleDateString("en-US", { month: "short", year: "numeric" });

  return (
    <div style={{
      background: "var(--coconut)",
      border: `4px solid ${accentColor}`,
      borderRadius: "50%",
      aspectRatio: "1 / 1",
      display: "flex",
      flexDirection: "column",
      alignItems: "center",
      justifyContent: "center",
      padding: "1.25rem",
      gap: "0.3rem",
      textAlign: "center",
    }}>
      <div style={{ fontWeight: 700, color: "var(--text)", fontSize: "0.95rem" }}>
        {member.displayName}{fruit ? ` ${fruit.emoji}` : ""}
      </div>
      {member.funFact && (
        <p style={{ margin: 0, fontSize: "0.78rem", color: "var(--muted)", fontStyle: "italic", lineHeight: 1.4 }}>
          fun fact: {member.funFact}
        </p>
      )}
      <p style={{ margin: 0, fontSize: "0.72rem", color: "var(--muted)", marginTop: "0.2rem" }}>
        since {joinDate}
      </p>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  background: "var(--bg)",
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-input)",
  color: "var(--text)",
  padding: "0.6rem 0.9rem",
  fontFamily: "var(--font-body)",
  fontSize: "0.9rem",
  outline: "none",
  width: "100%",
};

function buttonStyle(color: string): React.CSSProperties {
  return {
    background: color,
    border: "none",
    borderRadius: "var(--radius-pill)",
    color: "#000",
    fontFamily: "var(--font-display)",
    fontSize: "1rem",
    padding: "0.6rem 1.25rem",
    cursor: "pointer",
    fontWeight: 700,
  };
}
