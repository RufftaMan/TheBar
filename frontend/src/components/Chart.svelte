<script lang="ts">
  // Small line chart (1-2 series) with a crosshair tooltip. One y-axis only.
  import { clockTime } from '../lib/format';

  type Series = { name: string; values: number[]; color: string };
  let {
    title,
    series,
    times,
    format,
    min = 1,
    value = '',
    sub = '',
  }: {
    title: string;
    series: Series[];
    times: number[];
    format: (v: number) => string;
    min?: number;
    value?: string;
    sub?: string; // small caption under the title, where a two-series chart has its legend
  } = $props();

  const W = 300;
  const H = 90;
  let hover = $state<number | null>(null);
  let el: SVGSVGElement;

  const n = $derived(Math.max(0, ...series.map((s) => s.values.length)));
  const max = $derived(Math.max(min, ...series.flatMap((s) => s.values)) * 1.15);
  const x = (i: number) => (n < 2 ? W : (i / (n - 1)) * W);
  const y = (v: number) => H - (v / max) * H;
  function path(vals: number[]) {
    return vals.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('');
  }
  function move(e: PointerEvent) {
    if (n < 2) return;
    const r = el.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * W;
    hover = Math.max(0, Math.min(n - 1, Math.round((px / W) * (n - 1))));
  }
  const spanMin = $derived(times.length > 1 ? Math.round((times[times.length - 1] - times[0]) / 60000) : 0);
</script>

<figure class="chart">
  <figcaption>
    <span class="t">{title}</span>
    <span class="v num">{value}</span>
  </figcaption>
  {#if series.length > 1}
    <div class="legend">
      {#each series as s}
        <span><i style="background:{s.color}"></i>{s.name}</span>
      {/each}
    </div>
  {:else}
    <div class="legend sub" title={sub}>{sub}</div>
  {/if}
  <div class="plot">
    <svg
      bind:this={el}
      viewBox="0 0 {W} {H}"
      preserveAspectRatio="none"
      role="img"
      aria-label="{title} over the last {spanMin || 'few'} minutes"
      onpointermove={move}
      onpointerleave={() => (hover = null)}>
      <line x1="0" x2={W} y1={H / 2} y2={H / 2} class="grid" vector-effect="non-scaling-stroke" />
      <line x1="0" x2={W} y1={H} y2={H} class="base" vector-effect="non-scaling-stroke" />
      {#if n > 1}
        {#each series as s}
          {#if series.length === 1}
            <path d="{path(s.values)}L{W},{H}L0,{H}Z" fill={s.color} opacity="0.1" />
          {/if}
          <path d={path(s.values)} fill="none" stroke={s.color} stroke-width="2" vector-effect="non-scaling-stroke" stroke-linejoin="round" />
        {/each}
        {#if hover !== null}
          <line x1={x(hover)} x2={x(hover)} y1="0" y2={H} class="cross" vector-effect="non-scaling-stroke" />
        {/if}
      {/if}
    </svg>
    <span class="ymax num">{format(max / 1.15)}</span>
    {#if hover !== null}
      <div class="tip" style="left:{(x(hover) / W) * 100}%">
        <div class="muted">{clockTime(times[hover] || 0)}</div>
        {#each series as s}
          <div class="row"><i style="background:{s.color}"></i>{s.name}<b class="num">{format(s.values[hover] ?? 0)}</b></div>
        {/each}
      </div>
    {/if}
  </div>
  <div class="axis muted"><span>{spanMin ? `${spanMin} min ago` : ''}</span><span>now</span></div>
</figure>

<style>
  .chart {
    margin: 0;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px 12px 8px;
    min-width: 0;
  }
  figcaption {
    display: flex;
    flex-wrap: wrap; /* a long value drops to its own line instead of breaking mid-value */
    justify-content: space-between;
    align-items: baseline;
    gap: 0 8px;
  }
  .t {
    font-size: 12px;
    color: var(--text-2);
    font-weight: 600;
    white-space: nowrap;
  }
  .v {
    font-size: 15px;
    font-weight: 600;
    white-space: nowrap;
    margin-left: auto;
  }
  .sub {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-3);
  }
  .legend {
    display: flex;
    gap: 12px;
    min-height: 16px; /* same height with a legend or a caption, so the plots line up */
    align-items: center;
    font-size: 11px;
    color: var(--text-2);
    margin-top: 2px;
  }
  .legend i,
  .row i {
    display: inline-block;
    width: 10px;
    height: 3px;
    border-radius: 2px;
    margin-right: 5px;
    vertical-align: middle;
  }
  .plot {
    position: relative;
    height: 90px;
    margin-top: 8px;
  }
  svg {
    width: 100%;
    height: 100%;
    display: block;
    overflow: visible;
    touch-action: none;
  }
  .grid {
    stroke: var(--border);
    stroke-dasharray: 2 4;
  }
  .base {
    stroke: var(--border-strong);
  }
  .cross {
    stroke: var(--text-3);
  }
  .ymax {
    position: absolute;
    top: -2px;
    left: 0;
    font-size: 10px;
    color: var(--text-3);
    background: var(--surface);
    padding-right: 4px;
  }
  .axis {
    display: flex;
    justify-content: space-between;
    font-size: 10px;
    margin-top: 4px;
  }
  .tip {
    position: absolute;
    top: -6px;
    transform: translate(-50%, -100%);
    background: var(--surface-3);
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    padding: 6px 8px;
    font-size: 11px;
    pointer-events: none;
    white-space: nowrap;
    box-shadow: var(--shadow);
    z-index: 10;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 2px;
    color: var(--text-2);
  }
  .row b {
    margin-left: auto;
    padding-left: 10px;
    color: var(--text);
    font-weight: 600;
  }
</style>
