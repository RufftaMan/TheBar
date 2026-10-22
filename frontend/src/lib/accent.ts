// Picks an accent color from a logo, so every card gets its own tint.
// Runs on a tiny canvas; results are cached in memory and localStorage.

const cache = new Map<string, string>();
const LS_KEY = 'thebar.accents.v1';

try {
  const saved = JSON.parse(localStorage.getItem(LS_KEY) || '{}');
  for (const [k, v] of Object.entries(saved)) cache.set(k, v as string);
} catch {
  /* storage unavailable: fine */
}

function persist() {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(Object.fromEntries(cache)));
  } catch {
    /* ignore */
  }
}

/** Stable fallback hue from a name. */
export function hashAccent(name: string): string {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return `oklch(0.74 0.13 ${h % 360})`;
}

function rgbToOklch(r: number, g: number, b: number): [number, number, number] {
  const lin = (c: number) => {
    c /= 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  const R = lin(r), G = lin(g), B = lin(b);
  const l = Math.cbrt(0.4122214708 * R + 0.5363325363 * G + 0.0514459929 * B);
  const m = Math.cbrt(0.2119034982 * R + 0.6806995451 * G + 0.1073969566 * B);
  const s = Math.cbrt(0.0883024619 * R + 0.2817188376 * G + 0.6299787005 * B);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const a = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const bb = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  const C = Math.sqrt(a * a + bb * bb);
  const H = ((Math.atan2(bb, a) * 180) / Math.PI + 360) % 360;
  return [L, C, H];
}

/**
 * Most common saturated hue in the image, normalized so it reads well on a
 * dark surface (lightness and chroma clamped).
 */
export function accentFromImage(img: HTMLImageElement, key: string): string | null {
  if (cache.has(key)) return cache.get(key)!;
  try {
    const size = 32;
    const cv = document.createElement('canvas');
    cv.width = cv.height = size;
    const ctx = cv.getContext('2d', { willReadFrequently: true });
    if (!ctx) return null;
    ctx.drawImage(img, 0, 0, size, size);
    const data = ctx.getImageData(0, 0, size, size).data;
    const bins = new Map<number, { w: number; c: number }>();
    for (let i = 0; i < data.length; i += 4) {
      if (data[i + 3] < 128) continue;
      const [L, C, H] = rgbToOklch(data[i], data[i + 1], data[i + 2]);
      if (C < 0.05 || L < 0.25 || L > 0.97) continue; // skip greys, black, white
      const bin = Math.round(H / 15);
      const e = bins.get(bin) || { w: 0, c: 0 };
      e.w += C; // weight by colorfulness
      e.c = Math.max(e.c, C);
      bins.set(bin, e);
    }
    let best: [number, { w: number; c: number }] | null = null;
    for (const e of bins) if (!best || e[1].w > best[1].w) best = e;
    if (!best) {
      cache.set(key, '');
      persist();
      return null;
    }
    const hue = (best[0] * 15) % 360;
    const chroma = Math.min(0.17, Math.max(0.09, best[1].c));
    const out = `oklch(0.75 ${chroma.toFixed(3)} ${hue})`;
    cache.set(key, out);
    persist();
    return out;
  } catch {
    return null; // tainted canvas etc.
  }
}

export function cachedAccent(key: string): string | null | undefined {
  if (!cache.has(key)) return undefined;
  return cache.get(key) || null;
}
