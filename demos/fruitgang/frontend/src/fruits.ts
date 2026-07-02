export interface FruitMeta {
  emoji: string;
  label: string;
  colorVar: string; // CSS custom property name, e.g. "--strawberry"
}

// Keyed by the token suffix (the part after "#"), e.g. "strawberry"
// The lexicon stores full slugs like "community.fruitgang.log#strawberry".
// Use fruitSlug(fullSlug) to get the key.
export const FRUITS: Record<string, FruitMeta> = {
  greenApple: { emoji: "🍏", label: "Green Apple", colorVar: "--lime" },
  redApple: { emoji: "🍎", label: "Red Apple", colorVar: "--cherry" },
  pear: { emoji: "🍐", label: "Pear", colorVar: "--lime" },
  tangerine: { emoji: "🍊", label: "Tangerine", colorVar: "--tangerine" },
  lemon: { emoji: "🍋", label: "Lemon", colorVar: "--banana" },
  lime: { emoji: "🍋‍🟩", label: "Lime", colorVar: "--lime" },
  banana: { emoji: "🍌", label: "Banana", colorVar: "--banana" },
  watermelon: { emoji: "🍉", label: "Watermelon", colorVar: "--strawberry" },
  grapes: { emoji: "🍇", label: "Grapes", colorVar: "--grape" },
  strawberry: { emoji: "🍓", label: "Strawberry", colorVar: "--strawberry" },
  blueberry: { emoji: "🫐", label: "Blueberry", colorVar: "--blueberry" },
  melon: { emoji: "🍈", label: "Melon", colorVar: "--coconut" },
  cherries: { emoji: "🍒", label: "Cherries", colorVar: "--cherry" },
  peach: { emoji: "🍑", label: "Peach", colorVar: "--peach" },
  mango: { emoji: "🥭", label: "Mango", colorVar: "--tangerine" },
  pineapple: { emoji: "🍍", label: "Pineapple", colorVar: "--banana" },
  coconut: { emoji: "🥥", label: "Coconut", colorVar: "--coconut" },
  kiwi: { emoji: "🥝", label: "Kiwi", colorVar: "--lime" },
  tomato: { emoji: "🍅", label: "Tomato", colorVar: "--cherry" },
  olive: { emoji: "🫒", label: "Olive", colorVar: "--lime" },
  avocado: { emoji: "🥑", label: "Avocado", colorVar: "--lime" },
};

// Strips the namespace prefix from a full lexicon token slug.
// "community.fruitgang.log#strawberry" → "strawberry"
export function fruitSlug(fullSlug: string): string {
  const idx = fullSlug.indexOf("#");
  return idx >= 0 ? fullSlug.slice(idx + 1) : fullSlug;
}

// Returns fruit metadata for a full lexicon token slug, or undefined if unknown.
export function getFruit(fullSlug: string): FruitMeta | undefined {
  return FRUITS[fruitSlug(fullSlug)];
}

export const FRUIT_KEYS = Object.keys(FRUITS);
