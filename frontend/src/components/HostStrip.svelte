<script lang="ts">
  import Icon from './Icon.svelte';
  import { app } from '../lib/store.svelte';
  import { bytes, duration, pct, rate } from '../lib/format';

  const h = $derived(app.host);
  const running = $derived(app.containers.filter((c) => c.state === 'running').length);
  const problems = $derived(app.containers.filter((c) => c.alert).length);

  function level(p: number) {
    return p >= 90 ? 'critical' : p >= 75 ? 'warning' : 'ok';
  }
</script>

{#if h}
  <div class="strip" aria-label="Host status">
    <div class="m" title="CPU: {pct(h.cpu)} of {h.cores} cores · load {h.load.map((l) => l.toFixed(2)).join(' / ')}">
      <span class="k">CPU</span>
      <span class="bar {level(h.cpu)}"><i style="width:{Math.min(100, h.cpu)}%"></i></span>
      <span class="v num">{pct(h.cpu, 0)}</span>
    </div>
    <div class="m" title="Memory: {bytes(h.memUsed)} of {bytes(h.memTotal)}">
      <span class="k">RAM</span>
      <span class="bar {level((h.memUsed / h.memTotal) * 100)}"><i style="width:{(h.memUsed / h.memTotal) * 100}%"></i></span>
      <span class="v num">{pct((h.memUsed / h.memTotal) * 100, 0)}</span>
    </div>
    {#each h.disks || [] as d (d.path)}
      {#if d.problem}
        <div class="m" title="{d.label} ({d.path}): {d.problem}">
          <span class="k">{d.label}</span>
          <span class="bad"><Icon name="alert" size={12} stroke={2.4} />unavailable</span>
        </div>
      {:else}
        {@const p = (d.used / d.total) * 100}
        <div class="m" title="{d.label} ({d.path}): {bytes(d.used)} of {bytes(d.total)} used">
          <span class="k">{d.label}</span>
          <span class="bar {level(p)}"><i style="width:{p}%"></i></span>
          <span class="v num">{pct(p, 0)}</span>
        </div>
      {/if}
    {/each}
    {#if h.iface}
      <div class="m net" title="Network on {h.iface}">
        <span class="k">NET</span>
        <span class="v num">↓ {rate(h.rx)}</span>
        <span class="v num">↑ {rate(h.tx)}</span>
      </div>
    {/if}
    <div class="m" title="{h.hostname} has been up for {duration(h.uptime * 1000)}">
      <span class="k">UP</span>
      <span class="v num">{duration(h.uptime * 1000)}</span>
    </div>
    <div class="m" title="{running} of {app.containers.length} containers running{problems ? `, ${problems} with problems` : ''}">
      <span class="k">CTRS</span>
      <span class="v num">{running}/{app.containers.length}</span>
      {#if problems}<span class="prob">{problems} ✕</span>{/if}
    </div>
  </div>
{/if}

<style>
  .strip {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 18px;
    align-items: center;
    font-size: 12px;
  }
  .m {
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .k {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.07em;
    color: var(--text-3);
    text-transform: uppercase;
  }
  .v {
    color: var(--text-2);
  }
  .bar {
    width: 54px;
    height: 6px;
    border-radius: 4px;
    background: rgba(245, 165, 36, 0.16);
    overflow: hidden;
  }
  .bar i {
    display: block;
    height: 100%;
    border-radius: 4px;
    background: var(--brand);
    transition: width 0.6s ease;
  }
  .bar.warning {
    background: rgba(250, 178, 25, 0.18);
  }
  .bar.warning i {
    background: var(--warning);
  }
  .bar.critical {
    background: rgba(208, 59, 59, 0.2);
  }
  .bar.critical i {
    background: var(--critical);
  }
  .bad {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: #f19a9a;
    font-weight: 600;
  }
  .bad :global(svg) {
    color: var(--critical);
  }
  .prob {
    color: #f19a9a;
    font-weight: 600;
  }
</style>
