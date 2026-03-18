import { useState } from "react";

export default function WaitlistForm() {
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
          body: JSON.stringify({ email }),
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
      {status === "invalid" && (
        <p className="text-xs text-gray-700">
          That email wasn't quite right. Please submit an email like
          abc@example.com.
        </p>
      )}
      {status === "error" && (
        <p className="text-xs text-gray-700">
          Hmm, that didn't work. Please try again.
        </p>
      )}
      {status === "success" && (
        <p className="text-xs" style={{ color: "#2A7047" }}>
          Thanks for signing up! We can't wait to show you what we've been
          growing 🌱
        </p>
      )}
    </div>
  );
}
