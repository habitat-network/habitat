import Link from "next/link";

export default function Home() {
  return (
    <main
      className="flex items-center justify-center w-full h-screen flex-col gap-4"
    >
      <div className="text-5xl flex flex-col items-center">
        <span>🌱</span>
        <h1>Habitat</h1>
      </div>
      <nav className="underline">
        <ul>
          <li>
            <Link href="/test">Test Route</Link>
          </li>
        </ul>
      </nav>
    </main>
  );
}

