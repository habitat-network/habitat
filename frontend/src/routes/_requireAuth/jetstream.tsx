import React from "react";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/jetstream")({
  component: JetstreamViewer,
});

interface SSEEvent {
  id: number;
  data: string;
  timestamp: string;
}

function JetstreamViewer() {
  const [events, setEvents] = React.useState<SSEEvent[]>([]);
  const [status, setStatus] = React.useState<"connecting" | "open" | "closed">(
    "connecting",
  );
  const [error, setError] = React.useState<string | null>(null);
  const counterRef = React.useRef(0);

  React.useEffect(() => {
    const url = `https://${__HABITAT_DOMAIN__}/jetstream`;
    const source = new EventSource(url);

    source.onopen = () => {
      setStatus("open");
      setError(null);
    };

    source.onmessage = (e) => {
      const id = ++counterRef.current;
      setEvents((prev) => [
        { id, data: e.data, timestamp: new Date().toISOString() },
        ...prev.slice(0, 199),
      ]);
    };

    source.onerror = () => {
      setStatus("closed");
      setError("Connection lost. EventSource will retry automatically.");
    };

    return () => {
      source.close();
      setStatus("closed");
    };
  }, []);

  return (
    <div style={{ padding: "1.5rem", fontFamily: "monospace" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "0.75rem",
          marginBottom: "1rem",
        }}
      >
        <h1 style={{ margin: 0, fontSize: "1.5rem", fontFamily: "sans-serif" }}>
          Jetstream
        </h1>
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: "0.375rem",
            fontSize: "0.8rem",
            color:
              status === "open"
                ? "green"
                : status === "connecting"
                  ? "orange"
                  : "red",
          }}
        >
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              backgroundColor: "currentColor",
              display: "inline-block",
            }}
          />
          {status}
        </span>
        <span
          style={{ fontSize: "0.75rem", color: "GrayText", marginLeft: "auto" }}
        >
          {events.length} event{events.length !== 1 ? "s" : ""}
        </span>
        <button
          style={{
            fontSize: "0.75rem",
            background: "none",
            border: "1px solid ButtonBorder",
            borderRadius: "4px",
            cursor: "pointer",
            padding: "0.2rem 0.5rem",
          }}
          onClick={() => {
            setEvents([]);
            counterRef.current = 0;
          }}
        >
          Clear
        </button>
      </div>

      {error && (
        <div
          style={{
            marginBottom: "0.75rem",
            padding: "0.5rem 0.75rem",
            backgroundColor: "rgba(220,38,38,0.1)",
            border: "1px solid rgba(220,38,38,0.3)",
            borderRadius: "4px",
            fontSize: "0.8rem",
            color: "red",
          }}
        >
          {error}
        </div>
      )}

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "0.375rem",
          maxHeight: "calc(100vh - 160px)",
          overflowY: "auto",
        }}
      >
        {events.length === 0 && status === "open" && (
          <div style={{ color: "GrayText", fontSize: "0.875rem" }}>
            Waiting for events…
          </div>
        )}
        {events.map((ev) => (
          <div
            key={ev.id}
            style={{
              display: "grid",
              gridTemplateColumns: "11rem 1fr",
              gap: "0.75rem",
              padding: "0.375rem 0.5rem",
              borderRadius: "4px",
              backgroundColor: "Canvas",
              border: "1px solid rgba(128,128,128,0.15)",
              fontSize: "0.78rem",
              colorScheme: "light dark",
            }}
          >
            <span style={{ color: "GrayText", flexShrink: 0 }}>
              {ev.timestamp}
            </span>
            <pre
              style={{
                margin: 0,
                whiteSpace: "pre-wrap",
                wordBreak: "break-all",
                color: "FieldText",
              }}
            >
              {(() => {
                try {
                  return JSON.stringify(JSON.parse(ev.data), null, 2);
                } catch {
                  return ev.data;
                }
              })()}
            </pre>
          </div>
        ))}
      </div>
    </div>
  );
}
