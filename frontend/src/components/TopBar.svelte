<script lang="ts">
  import Icon from './Icon.svelte';
  import HostStrip from './HostStrip.svelte';
  import { app } from '../lib/store.svelte';

  let { onkey, input = $bindable() }: { onkey: (e: KeyboardEvent) => void; input?: HTMLInputElement } = $props();
</script>

<header class="top">
  <div class="row">
    <a class="brand" href="/" aria-label="TheBar home">
      <img src="/favicon.svg" alt="" width="30" height="30" />
      <span>The<b>Bar</b></span>
    </a>
    <div class="search" class:active={!!app.query}>
      <Icon name="search" size={16} />
      <input
        bind:this={input}
        bind:value={app.query}
        onkeydown={onkey}
        placeholder="Type to find & launch…"
        aria-label="Search services"
        autocomplete="off"
        spellcheck="false" />
      {#if app.query}
        <button class="btn sm icon ghost" aria-label="Clear search" onclick={() => ((app.query = ''), input?.focus())}><Icon name="x" size={14} /></button>
      {:else}
        <kbd class="hint">/</kbd>
      {/if}
    </div>
    <div class="buttons">
      {#if !app.connected}
        <span class="offline" role="status" aria-label="Live updates paused, reconnecting" title="Live updates paused, reconnecting…">
          <i></i><span class="txt">Reconnecting…</span>
        </span>
      {/if}
      <button class="btn icon" title="Ports in use" aria-label="Ports in use" onclick={() => (app.modal = { kind: 'ports' })}><Icon name="plug" size={15} /></button>
      <button class="btn icon" title="Settings" aria-label="Settings" onclick={() => (app.modal = { kind: 'settings' })}><Icon name="settings" size={15} /></button>
    </div>
  </div>
  <HostStrip />
</header>

<style>
  .top {
    position: sticky;
    top: 0;
    z-index: 40;
    display: grid;
    gap: 10px;
    padding: 12px var(--gutter) 12px;
    background: rgba(12, 14, 18, 0.82);
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
    border-bottom: 1px solid var(--border);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 9px;
    text-decoration: none;
    font-size: 19px;
    letter-spacing: -0.02em;
    font-weight: 500;
    flex-shrink: 0;
  }
  .brand b {
    color: var(--brand);
    font-weight: 750;
  }
  .row {
    min-width: 0;
  }
  .search {
    flex: 1;
    min-width: 0; /* may shrink below its content on narrow screens */
    max-width: 560px;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 38px;
    padding: 0 6px 0 12px;
    border-radius: 11px;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    color: var(--text-3);
    transition:
      border-color 0.12s,
      box-shadow 0.12s;
  }
  .search:focus-within,
  .search.active {
    border-color: var(--brand);
    box-shadow: 0 0 0 3px var(--brand-soft);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    background: none;
    padding: 0;
    box-shadow: none !important;
    outline: none;
    font-size: 14.5px;
  }
  kbd {
    font-family: var(--mono);
    font-size: 11px;
    border: 1px solid var(--border-strong);
    border-radius: 5px;
    padding: 1px 6px;
    color: var(--text-3);
  }
  .buttons {
    display: flex;
    gap: 6px;
    align-items: center;
    margin-left: auto;
    flex-shrink: 0;
  }
  .offline {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--warning);
    white-space: nowrap;
  }
  .offline i {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--warning);
    animation: pulse 1.4s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.3;
    }
  }
  @media (hover: none) {
    .hint {
      display: none; /* no "/" key on a touch screen */
    }
  }
  @media (max-width: 700px) {
    .brand span,
    .offline .txt {
      display: none; /* phones: just the amber dot */
    }
    .row {
      gap: 8px;
    }
  }
</style>
