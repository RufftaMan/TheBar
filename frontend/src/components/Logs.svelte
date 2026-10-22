<script lang="ts">
  import Icon from './Icon.svelte';
  import type { Container } from '../lib/types';

  let { containers, selected = $bindable('') }: { containers: Container[]; selected?: string } = $props();

  const MAX = 2000;
  type Line = { s: number; t: string; ts: string; id: number };
  let lines = $state<Line[]>([]);
  let filter = $state('');
  let follow = $state(true);
  let wrap = $state(true);
  let ended = $state(false);
  let box: HTMLDivElement;
  let seq = 0;

  $effect(() => {
    if (!selected || !containers.some((c) => c.id === selected)) {
      selected = (containers.find((c) => c.state === 'running') || containers[0])?.id || '';
    }
  });

  $effect(() => {
    const id = selected;
    if (!id) return;
    lines = [];
    ended = false;
    const es = new EventSource(`/api/containers/${id}/logs?tail=400`);
    es.addEventListener('lines', (e) => {
      const batch: { s: number; t: string }[] = JSON.parse((e as MessageEvent).data);
      const add = batch.map((l) => {
        // docker prefixes each line with an RFC3339 timestamp
        const sp = l.t.indexOf(' ');
        const iso = sp > 0 ? l.t.slice(0, sp) : '';
        const ok = /^\d{4}-\d{2}-\d{2}T/.test(iso);
        const ts = ok ? new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }) : '';
        return { s: l.s, t: ok ? l.t.slice(sp + 1) : l.t, ts, id: seq++ };
      });
      const next = lines.concat(add);
      lines = next.length > MAX ? next.slice(next.length - MAX) : next;
    });
    es.addEventListener('end', () => {
      ended = true;
      es.close();
    });
    return () => es.close();
  });

  const shown = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    return f ? lines.filter((l) => l.t.toLowerCase().includes(f)) : lines;
  });

  $effect(() => {
    shown.length;
    if (follow && box) queueMicrotask(() => (box.scrollTop = box.scrollHeight));
  });

  function onscroll() {
    const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 30;
    if (follow !== atBottom) follow = atBottom;
  }
</script>

<div class="logs">
  <div class="bar">
    {#if containers.length > 1}
      <select bind:value={selected} aria-label="Container">
        {#each containers as c (c.id)}
          <option value={c.id}>{c.name}{c.state !== 'running' ? ' (stopped)' : ''}</option>
        {/each}
      </select>
    {/if}
    <div class="filter">
      <Icon name="search" size={14} />
      <input placeholder="Filter lines" bind:value={filter} aria-label="Filter log lines" />
    </div>
    <label class="tog"><input type="checkbox" bind:checked={wrap} /> Wrap</label>
    <button class="btn sm" class:active={follow} onclick={() => (follow = !follow)} title="Keep scrolled to the newest line">
      <Icon name="chevronDown" size={13} />Follow
    </button>
    <button class="btn sm ghost" onclick={() => (lines = [])}>Clear</button>
  </div>
  <div class="box" class:wrap bind:this={box} {onscroll}>
    {#each shown as l (l.id)}
      <div class="ln" class:err={l.s === 2}><span class="ts">{l.ts}</span>{l.t}</div>
    {:else}
      <div class="muted pad">{filter ? 'No matching lines.' : 'No log output yet.'}</div>
    {/each}
    {#if ended}
      <div class="muted pad">— stream ended (container stopped) —</div>
    {/if}
  </div>
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin-bottom: 8px;
  }
  select {
    max-width: 260px;
    height: 30px;
    padding: 0 8px;
  }
  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 160px;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 0 8px;
    color: var(--text-3);
  }
  .filter input {
    border: none;
    background: none;
    padding: 5px 0;
    flex: 1;
    min-width: 0;
    box-shadow: none;
  }
  .tog {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-2);
  }
  .box {
    height: 420px;
    overflow: auto;
    background: #090b0e;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 0;
    font-family: var(--mono);
    font-size: 12px;
    line-height: 1.5;
  }
  .ln {
    padding: 0 12px;
    white-space: pre;
    color: #cfd4dc;
  }
  .wrap .ln {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .ln:hover {
    background: #12161c;
  }
  .ln.err {
    color: #f3b0a2;
  }
  .ts {
    color: #5c6370;
    margin-right: 10px;
    user-select: none;
  }
  .pad {
    padding: 4px 12px;
  }
</style>
