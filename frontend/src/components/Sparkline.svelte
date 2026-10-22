<script lang="ts">
  // Tiny single-series trend line. The number next to it carries the value;
  // the line only shows shape over the last few minutes.
  let {
    values,
    color = 'var(--series-1)',
    min = 1,
    height = 26,
    label = '',
    format = (v: number) => String(v),
  }: {
    values: number[];
    color?: string;
    min?: number; // minimum top of the y-scale, so idle noise stays flat
    height?: number;
    label?: string;
    format?: (v: number) => string;
  } = $props();

  const W = 100;
  let hover = $state<number | null>(null);
  let el: SVGSVGElement;

  const max = $derived(Math.max(min, ...values) * 1.1);
  const pts = $derived(
    values.map((v, i) => [values.length < 2 ? W : (i / (values.length - 1)) * W, height - 2 - (v / max) * (height - 4)] as const),
  );
  const line = $derived(pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(2)},${p[1].toFixed(2)}`).join(''));
  const area = $derived(pts.length ? `${line}L${W},${height}L${pts[0][0]},${height}Z` : '');

  function move(e: PointerEvent) {
    if (!values.length) return;
    const r = el.getBoundingClientRect();
    const x = ((e.clientX - r.left) / r.width) * W;
    hover = Math.max(0, Math.min(values.length - 1, Math.round((x / W) * (values.length - 1))));
  }
</script>

<div class="spark" style="height:{height}px">
  <svg
    bind:this={el}
    viewBox="0 0 {W} {height}"
    preserveAspectRatio="none"
    role="img"
    aria-label={label}
    onpointermove={move}
    onpointerleave={() => (hover = null)}>
    {#if pts.length > 1}
      <path d={area} fill={color} opacity="0.12" />
      <path d={line} fill="none" stroke={color} stroke-width="1.5" vector-effect="non-scaling-stroke" stroke-linejoin="round" />
      {#if hover !== null}
        <line x1={pts[hover][0]} x2={pts[hover][0]} y1="0" y2={height} stroke="var(--text-3)" stroke-width="1" vector-effect="non-scaling-stroke" />
      {/if}
    {:else}
      <line x1="0" x2={W} y1={height - 2} y2={height - 2} stroke="var(--border-strong)" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="2 3" />
    {/if}
  </svg>
  {#if hover !== null && values.length}
    <div class="tip num" style="left:{(pts[hover][0] / W) * 100}%">{format(values[hover])}</div>
  {/if}
</div>

<style>
  .spark {
    position: relative;
    width: 100%;
  }
  svg {
    width: 100%;
    height: 100%;
    display: block;
    overflow: visible;
  }
  .tip {
    position: absolute;
    bottom: calc(100% + 4px);
    transform: translateX(-50%);
    background: var(--surface-3);
    border: 1px solid var(--border-strong);
    color: var(--text);
    font-size: 11px;
    padding: 2px 6px;
    border-radius: 6px;
    pointer-events: none;
    white-space: nowrap;
    z-index: 5;
  }
</style>
