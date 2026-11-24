<script lang="ts">
  import Icon from './Icon.svelte';
  import type { Snippet } from 'svelte';

  let { title, onclose, wide = false, children }: { title: string; onclose: () => void; wide?: boolean; children: Snippet } = $props();

  // Only close when the press *started* on the backdrop too: dragging a text
  // selection out of an input and releasing outside the dialog fires a click
  // on the backdrop, which must not count.
  let pressedOnScrim = false;
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div
  class="scrim"
  role="presentation"
  onmousedown={(e) => (pressedOnScrim = e.target === e.currentTarget)}
  onclick={(e) => e.target === e.currentTarget && pressedOnScrim && onclose()}>
  <div class="dlg" class:wide role="dialog" aria-modal="true" aria-label={title}>
    <header>
      <h2>{title}</h2>
      <button class="btn icon ghost" aria-label="Close" onclick={onclose}><Icon name="x" /></button>
    </header>
    <div class="body">
      {@render children()}
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgba(5, 6, 8, 0.6);
    backdrop-filter: blur(3px);
    display: grid;
    place-items: start center;
    padding: 6vh var(--gutter) var(--gutter);
    overflow: auto;
  }
  .dlg {
    width: min(640px, 100%);
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: calc(var(--radius) + 2px);
    box-shadow: 0 30px 80px rgba(0, 0, 0, 0.5);
  }
  .dlg.wide {
    width: min(1100px, 100%);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 12px 12px 18px;
    border-bottom: 1px solid var(--border);
  }
  h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 650;
  }
  .body {
    padding: 16px 18px 18px;
  }
</style>
