import { useEffect, useRef } from "react";

const IMAGE_HEIGHT = 200; // px — adjust here

interface CarouselProps {
  items: { image: string; alt?: string; href?: string }[];
}

export default function Carousel({ items }: CarouselProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const animRef = useRef<number>(0);
  const posRef = useRef<number>(0);
  const pauseUntilRef = useRef<number>(0);

  const doubled = [...items, ...items];

  useEffect(() => {
    const track = trackRef.current;
    const wrapper = wrapperRef.current;
    if (!track || !wrapper) return;

    const step = (timestamp: number) => {
      const halfWidth = track.scrollWidth / 2;
      if (timestamp > pauseUntilRef.current) {
        posRef.current += 0.8; // px per frame — adjust speed here
      }
      if (posRef.current >= halfWidth) posRef.current -= halfWidth;
      if (posRef.current < 0) posRef.current += halfWidth;
      track.style.transform = `translateX(-${posRef.current}px)`;
      animRef.current = requestAnimationFrame(step);
    };

    animRef.current = requestAnimationFrame(step);

    const handleWheel = (e: WheelEvent) => {
      e.preventDefault();
      const track = trackRef.current;
      if (!track) return;
      const halfWidth = track.scrollWidth / 2;
      posRef.current += e.deltaX || e.deltaY;
      if (posRef.current >= halfWidth) posRef.current -= halfWidth;
      if (posRef.current < 0) posRef.current += halfWidth;
      // Pause auto-scroll for 1 second after user interaction
      pauseUntilRef.current = performance.now() + 1000;
    };

    wrapper.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      cancelAnimationFrame(animRef.current);
      wrapper.removeEventListener("wheel", handleWheel);
    };
  }, []);

  return (
    <div ref={wrapperRef} className="overflow-hidden">
      <div
        ref={trackRef}
        className="flex will-change-transform"
        style={{ height: IMAGE_HEIGHT }}
      >
        {doubled.map((item, i) => (
          <a
            key={i}
            href={item.href ?? "#"}
            target="_blank"
            rel="noopener noreferrer"
            className="shrink-0 block"
            style={{ height: IMAGE_HEIGHT }}
          >
            <img
              src={item.image}
              alt={item.alt ?? ""}
              style={{ height: IMAGE_HEIGHT, width: "auto", display: "block" }}
            />
          </a>
        ))}
      </div>
    </div>
  );
}
