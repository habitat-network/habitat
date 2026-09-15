import { createFileRoute, Link } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useQuery, useMutation } from "convex/react";
import { useState } from "react";
import { api } from "../../convex/_generated/api";
import { useAppSession } from "~/server/session";

const whoamiFn = createServerFn({ method: "GET" }).handler(async () => {
  const session = await useAppSession();
  return { did: session.data.did ?? null };
});

export const Route = createFileRoute("/")({
  loader: () => whoamiFn(),
  component: Home,
});

function Home() {
  const { did } = Route.useLoaderData();
  const records = useQuery(api.records.list, {});
  const write = useMutation(api.records.write);
  const [subject, setSubject] = useState("");
  const [relation, setRelation] = useState("writer");
  const [space, setSpace] = useState("");

  if (!did) {
    return (
      <p>
        Not signed in. <Link to="/login">Sign in</Link>
      </p>
    );
  }

  return (
    <div style={{ maxWidth: 640, margin: "2rem auto" }}>
      <p>Signed in as {did}</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void write({ space, repo: did, subject, relation });
        }}
      >
        <input placeholder="space at-uri" value={space} onChange={(e) => setSpace(e.target.value)} />
        <input placeholder="subject did" value={subject} onChange={(e) => setSubject(e.target.value)} />
        <input placeholder="relation" value={relation} onChange={(e) => setRelation(e.target.value)} />
        <button type="submit">Write</button>
      </form>
      <ul>
        {(records ?? []).map((r) => (
          <li key={r._id}>
            {r.subject} — {r.relation} ({r.syncStatus})
          </li>
        ))}
      </ul>
    </div>
  );
}
