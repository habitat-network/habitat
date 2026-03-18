import { useState } from "react";

interface WaitlistFormProps {
  from: "user" | "developer" | "index";
  label?: string;
}

export default function WaitlistForm({ from, label }: WaitlistFormProps) {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<
    "idle" | "success" | "invalid" | "error"
  >("idle");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setStatus("idle");
    try {
      const res = await fetch(
        "https://habitat-953995456319.us-west1.run.app/waitlist",
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email, from }),
        },
      );
      if (res.ok) {
        setStatus("success");
      } else {
        const text = await res.text();
        setStatus(
          text.trim().toLowerCase() === "invalid email address"
            ? "invalid"
            : "error",
        );
      }
    } catch {
      setStatus("error");
    }
  };

  return (
    <div className="flex flex-col items-center gap-2">
      <div className="flex items-center gap-8 flex-wrap justify-center">
        {label && <div className="prose"><h3 style={{ margin: 0 }}>{label}</h3></div>}
        <form
          onSubmit={handleSubmit}
          className="flex items-center gap-3 flex-wrap justify-center"
        >
          <input
            type="text"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@example.com"
            className="border border-gray-300 rounded-lg px-4 py-2 text-base outline-none focus:border-green-600"
            style={{ minWidth: "260px" }}
          />
          <button
            type="submit"
            className="rounded-lg px-5 py-2 text-base font-medium text-white cursor-pointer transition-opacity hover:opacity-80"
            style={{ backgroundColor: "#2A7047" }}
          >
            Sign me up!
          </button>
        </form>
      </div>
      <p className="text-xs text-center h-4" style={{ color: status === "success" ? "#2A7047" : "#374151" }}>
        {status === "invalid" && "That email wasn't quite right. Please submit an email like abc@example.com."}
        {status === "error" && "Hmm, that didn't work. Please try again."}
        {status === "success" && "Thanks for signing up! We can't wait to show you what we've been growing 🌱"}
      </p>
    </div>
  );
}
