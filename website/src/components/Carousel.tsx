import { useRef } from "react";

interface CarouselProps {
  items: { image: string; alt?: string }[];
}

export default function Carousel({ items }: CarouselProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  const scroll = (direction: "prev" | "next") => {
    const container = scrollRef.current;
    if (!container) return;
    const slides = Array.from(container.children) as HTMLElement[];
    const currentScroll = container.scrollLeft;
    let target: HTMLElement | undefined;

    if (direction === "next") {
      target = slides.find((slide) => slide.offsetLeft > currentScroll + 1);
    } else {
      target = [...slides]
        .reverse()
        .find((slide) => slide.offsetLeft < currentScroll - 1);
    }

    if (target) {
      container.scrollTo({ left: target.offsetLeft, behavior: "smooth" });
    }
  };

  return (
    <div className="relative w-full group">
      <div
        ref={scrollRef}
        className="overflow-x-auto snap-x snap-mandatory flex"
        style={{ scrollbarWidth: "none" }}
      >
        {items.map((item, i) => (
          <div
            key={i}
            className="snap-start shrink-0 w-[65%] flex items-center justify-center pl-14"
          >
            <img
              src={item.image}
              alt={item.alt ?? ""}
              className="max-h-64 object-contain"
            />
          </div>
        ))}
      </div>
      <button
        onClick={() => scroll("prev")}
        className="absolute left-2 top-1/2 -translate-y-1/2 w-10 h-10 rounded-full bg-black/10 hover:bg-black/20 flex items-center justify-center text-lg cursor-pointer opacity-0 group-hover:opacity-100 transition-opacity"
        aria-label="Previous"
      >
        &larr;
      </button>
      <button
        onClick={() => scroll("next")}
        className="absolute right-2 top-1/2 -translate-y-1/2 w-10 h-10 rounded-full bg-black/10 hover:bg-black/20 flex items-center justify-center text-lg cursor-pointer opacity-0 group-hover:opacity-100 transition-opacity"
        aria-label="Next"
      >
        &rarr;
      </button>
    </div>
  );
}
