<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { app } from '../lib/store.svelte';

  type Used = { port: number; proto: string; owner: string; bind: string };
  let ports = $state<Used[]>([]);
  let loading = $state(true);
  let near = $state(8000);
  let filter = $state('');
  let proto = $state<'tcp' | 'udp'>('tcp');

  api.get<Used[]>('/api/ports').then((p) => {
    ports = p;
    loading = false;
  });

  const used = $derived(new Set(ports.filter((p) => p.proto === proto).map((p) => p.port)));
  const free = $derived.by(() => {
    const out: number[] = [];
    for (let p = Math.max(1024, near || 0); p < 65535 && out.length < 6; p++) if (!used.has(p)) out.push(p);
    return out;
  });
  const isFree = $derived(near > 0 && !used.has(near));
  const shown = $derived(
    ports.filter((p) => {
      const f = filter.trim().toLowerCase();
      return !f || String(p.port).includes(f) || p.owner.toLowerCase().includes(f) || p.proto.includes(f);
    }),
  );
  function ownerLabel(name: string) {
    return name || 'host process';
  }
</script>

<Modal title="Ports" onclose={() => (app.modal = null)}>
  <div class="finder">
    <label>
      <span class="lbl">Need a port for something new? Start looking at</span>
      <div class="row">
        <input type="number" min="1" max="65535" bind:value={near} />
        <select bind:value={proto}><option value="tcp">TCP</option><option value="udp">UDP</option></select>
        <span class="verdict" class:ok={isFree}>{isFree ? `${near} is free` : `${near} is taken`}</span>
      </div>
    </label>
    <div class="free">
      <span class="muted">Free:</span>
      {#each free as p}<button class="chip num" onclick={() => navigator.clipboard?.writeText(String(p)).then(() => app.toast(`Copied ${p}`, 'ok'))} title="Copy">{p}</button>{/each}
    </div>
  </div>

  <div class="listhead">
    <h3>In use <span class="muted">({ports.length})</span></h3>
    <input placeholder="Filter" bind:value={filter} aria-label="Filter ports" />
  </div>
  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <div class="table">
      <table>
        <thead><tr><th>Port</th><th>Proto</th><th>Used by</th><th>Listening on</th></tr></thead>
        <tbody>
          {#each shown as p (p.port + p.proto)}
            <tr class:stopped={p.bind === 'stopped'}>
              <td class="num">{p.port}</td>
              <td class="muted">{p.proto}</td>
              <td class:muted={!p.owner}>{ownerLabel(p.owner)}</td>
              <td class="muted">{p.bind === 'all' ? 'all interfaces' : p.bind === 'stopped' ? 'reserved (container stopped)' : p.bind}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</Modal>

<style>
  .finder {
    display: grid;
    gap: 10px;
    padding: 12px;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .lbl {
    font-size: 12.5px;
    color: var(--text-2);
  }
  .row {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: 6px;
    flex-wrap: wrap;
  }
  .row input {
    width: 110px;
  }
  .verdict {
    font-weight: 600;
    color: #f19a9a;
  }
  .verdict.ok {
    color: #7ad67a;
  }
  .free {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 13px;
  }
  .chip {
    border: 1px solid var(--border-strong);
    background: var(--surface);
    border-radius: 7px;
    padding: 2px 9px;
    cursor: pointer;
  }
  .chip:hover {
    border-color: var(--brand);
  }
  .listhead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin: 18px 0 8px;
  }
  h3 {
    margin: 0;
    font-size: 14px;
  }
  .listhead input {
    width: 180px;
    height: 30px;
  }
  .table {
    max-height: 50vh;
    overflow: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  th {
    text-align: left;
    font-size: 11px;
    color: var(--text-3);
    font-weight: 600;
    padding: 4px 8px;
    position: sticky;
    top: 0;
    background: var(--surface);
  }
  td {
    padding: 6px 8px;
    border-top: 1px solid var(--border);
  }
  tr.stopped td {
    opacity: 0.6;
  }
</style>
