<script lang="ts">
  import { accentFromImage, cachedAccent, hashAccent } from '../lib/accent';

  let {
    src,
    name,
    size = 48,
    accent = $bindable(),
  }: { src: string; name: string; size?: number; accent?: string } = $props();

  let failed = $state(false);
  let lastSrc = '';

  $effect(() => {
    if (src !== lastSrc) {
      lastSrc = src;
      failed = false;
    }
    const c = src ? cachedAccent(src) : undefined;
    if (c) accent = c;
    else if (c === null || !src || failed) accent = hashAccent(name);
  });

  function onload(e: Event) {
    const a = accentFromImage(e.currentTarget as HTMLImageElement, src);
    accent = a || hashAccent(name);
  }

  const initials = $derived(
    name
      .replace(/[^\p{L}\p{N} ]/gu, ' ')
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0].toUpperCase())
      .join('') || '?',
  );
</script>

{#if src && !failed}
  <img
    {src}
    alt=""
    width={size}
    height={size}
    loading="lazy"
    decoding="async"
    draggable="false"
    onload={onload}
    onerror={() => (failed = true)}
    style="width:{size}px;height:{size}px" />
{:else}
  <span class="mono-logo" style="width:{size}px;height:{size}px;font-size:{Math.round(size * 0.42)}px;--a:{accent || hashAccent(name)}"
    >{initials}</span>
{/if}

<style>
  img {
    object-fit: contain;
    display: block;
  }
  .mono-logo {
    display: grid;
    place-items: center;
    border-radius: 22%;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: #111;
    background: var(--a);
  }
</style>
