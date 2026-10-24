<script lang="ts">
  import Icon from './Icon.svelte';
  import Chart from './Chart.svelte';
  import Logs from './Logs.svelte';
  import CardSettings from './CardSettings.svelte';
  import { app } from '../lib/store.svelte';
  import { aggregate, last } from '../lib/stats';
  import { bytes, cores, pct, rate, since, timeAgo } from '../lib/format';
  import { cardAction, grafanaUrl, singleAction, updateCard } from '../lib/actions';
  import type { CardView, Container } from '../lib/types';

  let { card, accent }: { card: CardView; accent: string } = $props();

  type Tab = 'overview' | 'containers' | 'logs' | 'config' | 'settings';
  let tab = $state<Tab>('overview');
  let logTarget = $state<string>('');

  const color = $derived(accent || 'var(--brand)');
  const running = $derived(card.all.filter((c) => c.state === 'running'));
  const agg = $derived(aggregate(app.history, running.map((c) => c.id)));
  const anyStopped = $derived(card.all.some((c) => c.state !== 'running'));
  const grafana = $derived(grafanaUrl(card.main));
  const composeFile = $derived(card.main.configFiles?.[0]);
  const images = $derived([...new Set(card.all.map((c) => c.image))]);
  const disk = $derived({
    rw: card.all.reduce((s, c) => s + (c.sizeRw || 0), 0),
    vol: card.all.reduce((s, c) => s + (c.volSize || 0), 0),
  });

  let Editor: any = $state(null);
  $effect(() => {
    if (tab === 'config' && !Editor) import('./ComposeEditor.svelte').then((m) => (Editor = m.default));
  });

  function showLogs(c: Container) {
    logTarget = c.id;
    tab = 'logs';
  }
  function unfold(key: string) {
    app.updateLayout((l) => {
      l.cards[key] = { ...(l.cards[key] || {}), parent: '' };
    });
  }
  function uptimeOf(c: Container) {
    if (c.state === 'running') return 'Up ' + since(c.startedAt, app.now);
    if (c.finishedAt) return `${c.status}, ${timeAgo(Date.parse(c.finishedAt), app.now)}`;
    return c.status;
  }
  function cpuOf(id: string) {
    const h = app.history[id];
    return h?.length ? h[h.length - 1] : null;
  }
</script>

<div class="detail">
  <div class="toolbar">
    {#if card.url}
      <a class="btn primary" href={card.url} target="_blank" rel="noopener noreferrer"><Icon name="external" size={14} />Open</a>
    {/if}
    {#if (card.update || card.recreate) && card.project}
      <button
        class="btn update"
        onclick={() => updateCard(card)}
        title={card.update ? 'Pull the newer image and recreate' : 'The newer image is already downloaded: recreate to use it'}>
        <Icon name="download" size={14} />Update
      </button>
    {/if}
    {#if anyStopped}
      <button class="btn" onclick={() => cardAction(card, 'start')}><Icon name="play" size={14} />Start</button>
    {/if}
    {#if running.length}
      <button class="btn danger" onclick={() => cardAction(card, 'stop')}><Icon name="stop" size={14} />Stop</button>
      <button class="btn" onclick={() => cardAction(card, 'restart')}><Icon name="restart" size={14} />Restart</button>
    {/if}
    {#if card.drift && card.project}
      <button class="btn" onclick={() => (tab = 'config')}><Icon name="file" size={14} />Apply config…</button>
    {/if}
    {#if grafana}
      <a class="btn ghost" href={grafana} target="_blank" rel="noopener noreferrer" title="Open in Grafana"><Icon name="chart" size={14} />Grafana</a>
    {/if}
    <span class="spacer"></span>
    <button class="btn ghost icon" title="Close" aria-label="Close" onclick={() => (app.expanded = null)}><Icon name="x" /></button>
  </div>

  <div class="tabs" role="tablist">
    {#each [['overview', 'Overview'], ['containers', `Containers (${card.all.length})`], ['logs', 'Logs'], ['config', 'Config'], ['settings', 'Settings']] as [id, label]}
      <button role="tab" class:active={tab === id} aria-selected={tab === id} onclick={() => (tab = id as Tab)}>{label}</button>
    {/each}
  </div>

  <div class="panel">
    {#if tab === 'overview'}
      {#if running.length}
        <div class="charts">
          <Chart
            title="CPU"
            value={pct(last(agg.cpu))}
            sub="Whole machine · {cores(last(agg.cpu), app.host?.cores)}"
            series={[{ name: 'CPU', values: agg.cpu, color }]}
            times={agg.t}
            format={(v) => pct(v)}
            min={2} />
          <Chart
            title="Memory"
            value={bytes(last(agg.mem))}
            sub="Working set, without cache"
            series={[{ name: 'Memory', values: agg.mem, color }]}
            times={agg.t}
            format={(v) => bytes(v)}
            min={64e6} />
          {#if agg.hasNet}
            <Chart
              title="Network"
              value="↓ {rate(last(agg.rx))}  ↑ {rate(last(agg.tx))}"
              series={[
                { name: 'In', values: agg.rx, color: 'var(--series-1)' },
                { name: 'Out', values: agg.tx, color: 'var(--series-2)' },
              ]}
              times={agg.t}
              format={(v) => rate(v)}
              min={1000} />
          {/if}
          <Chart
            title="Disk I/O"
            value="R {rate(last(agg.br))}  W {rate(last(agg.bw))}"
            series={[
              { name: 'Read', values: agg.br, color: 'var(--series-1)' },
              { name: 'Write', values: agg.bw, color: 'var(--series-2)' },
            ]}
            times={agg.t}
            format={(v) => rate(v)}
            min={1000} />
        </div>
      {:else}
        <p class="muted empty">Not running. Start it to see live stats.</p>
      {/if}

      <dl class="info">
        {#if card.links.length}
          <dt>Links</dt>
          <dd class="links">
            {#each card.links as l}
              <a href={l.url} target="_blank" rel="noopener noreferrer" class="lnk"><Icon name="link" size={12} />{l.url.replace(/^https?:\/\//, '')}<span class="muted">{l.label}</span></a>
            {/each}
          </dd>
        {/if}
        <dt>Image{images.length > 1 ? 's' : ''}</dt>
        <dd class="mono small">{#each images as i}<div>{i}</div>{/each}</dd>
        {#if card.project}
          <dt>Compose</dt>
          <dd><span class="mono small">{composeFile}</span> <span class="muted">· project {card.project}, service {card.service}</span></dd>
        {/if}
        <dt>Status</dt>
        <dd>{uptimeOf(card.main)}{#if card.main.restartCount}<span class="muted"> · restarted {card.main.restartCount}×</span>{/if}{#if card.main.restartPolicy}<span class="muted"> · restart policy “{card.main.restartPolicy}”</span>{/if}</dd>
        <dt>Disk</dt>
        <dd>
          {#if disk.rw || disk.vol}
            {bytes(disk.rw)} container layer{#if disk.vol} · {bytes(disk.vol)} in volumes{/if}
          {:else}
            <span class="muted">Not measured yet (updated every 10 minutes)</span>
          {/if}
        </dd>
        {#if card.main.networkMode === 'host'}
          <dt>Network</dt>
          <dd>Host networking <span class="muted">(traffic isn't measured per container)</span></dd>
        {/if}
      </dl>
    {:else if tab === 'containers'}
      <table class="ctable">
        <thead>
          <tr><th>Container</th><th>Status</th><th class="r">CPU</th><th class="r">RAM</th><th></th></tr>
        </thead>
        <tbody>
          {#each [{ key: card.key, title: '', containers: card.own }, ...card.folded] as grp, gi}
            {#if gi > 0}
              <tr class="sep">
                <td colspan="5">
                  <span>{grp.title}</span>
                  <button class="btn sm ghost" onclick={() => unfold(grp.key)} title="Show as its own card"><Icon name="split" size={12} />Own card</button>
                </td>
              </tr>
            {/if}
            {#each grp.containers as c (c.id)}
              {@const s = cpuOf(c.id)}
              <tr>
                <td>
                  <div class="cname">{c.name}</div>
                  <div class="mono small muted">{c.image}</div>
                </td>
                <td>
                  <span class="cstate {c.alert ? 'critical' : c.state}">{c.alert || uptimeOf(c)}</span>
                </td>
                <td class="r num" title={s ? cores(s.cpu, app.host?.cores) : ''}>{s ? pct(s.cpu) : '–'}</td>
                <td class="r num">{s ? bytes(s.mem) : '–'}</td>
                <td class="r actions">
                  <button class="btn sm icon ghost" title="Logs" aria-label="Logs for {c.name}" onclick={() => showLogs(c)}><Icon name="logs" size={14} /></button>
                  {#if c.state === 'running'}
                    <button class="btn sm icon ghost" title="Restart" aria-label="Restart {c.name}" onclick={() => singleAction(c, 'restart')}><Icon name="restart" size={14} /></button>
                    {#if !c.self}
                      <button class="btn sm icon ghost" title="Stop" aria-label="Stop {c.name}" onclick={() => singleAction(c, 'stop')}><Icon name="stop" size={14} /></button>
                    {/if}
                  {:else}
                    <button class="btn sm icon ghost" title="Start" aria-label="Start {c.name}" onclick={() => singleAction(c, 'start')}><Icon name="play" size={14} /></button>
                  {/if}
                </td>
              </tr>
            {/each}
          {/each}
        </tbody>
      </table>
    {:else if tab === 'logs'}
      <Logs containers={card.all} bind:selected={logTarget} />
    {:else if tab === 'config'}
      {#if !card.project}
        <p class="muted empty">This container isn't managed by Docker Compose, so there's no config file to edit.</p>
      {:else if Editor}
        <Editor project={card.project} service={card.service} />
      {:else}
        <p class="muted empty">Loading editor…</p>
      {/if}
    {:else if tab === 'settings'}
      <CardSettings {card} />
    {/if}
  </div>
</div>

<style>
  .detail {
    padding: 0 14px 14px;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    padding-bottom: 12px;
  }
  .toolbar a {
    text-decoration: none;
  }
  .spacer {
    flex: 1;
  }
  /* translucent tint like Stop's hover state, always on; brighter on hover */
  .btn.update {
    background: rgba(57, 135, 229, 0.15);
    border-color: var(--series-1);
    color: #a9cdff;
    font-weight: 600;
  }
  .btn.update:hover:not(:disabled) {
    background: rgba(57, 135, 229, 0.3);
    border-color: #6aa8f2;
    color: #d6e8ff;
  }
  .tabs {
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs button {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    padding: 8px 12px;
    cursor: pointer;
    color: var(--text-3);
    font-size: 13px;
    font-weight: 500;
    white-space: nowrap;
    margin-bottom: -1px;
  }
  .tabs button:hover {
    color: var(--text);
  }
  .tabs button.active {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
  .panel {
    padding-top: 14px;
    min-width: 0;
  }
  .charts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 10px;
  }
  .empty {
    padding: 8px 0;
    margin: 0;
  }
  .info {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 8px 18px;
    margin: 16px 0 0;
    font-size: 13px;
  }
  dt {
    color: var(--text-3);
    font-size: 12px;
    padding-top: 1px;
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .small {
    font-size: 12px;
  }
  .links {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .lnk {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 8px;
    border-radius: 7px;
    background: var(--surface-2);
    border: 1px solid var(--border);
    text-decoration: none;
    font-size: 12px;
  }
  .lnk:hover {
    border-color: var(--accent);
  }
  .lnk .muted {
    font-size: 11px;
  }
  .ctable {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  .ctable th {
    text-align: left;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-3);
    letter-spacing: 0.04em;
    padding: 0 8px 6px;
  }
  .ctable td {
    padding: 7px 8px;
    border-top: 1px solid var(--border);
    vertical-align: middle;
  }
  .ctable .r {
    text-align: right;
  }
  .cname {
    font-weight: 500;
  }
  .sep td {
    background: var(--surface-2);
    font-size: 12px;
    color: var(--text-2);
    font-weight: 600;
  }
  .sep td {
    padding: 4px 8px;
  }
  .sep span {
    margin-right: 8px;
  }
  .actions {
    white-space: nowrap;
  }
  .cstate {
    font-size: 12px;
    color: var(--text-2);
  }
  .cstate.critical {
    color: #f19a9a;
  }
  .cstate.exited,
  .cstate.created {
    color: var(--text-3);
  }
  @media (max-width: 640px) {
    .info {
      grid-template-columns: 1fr;
      gap: 2px;
    }
    dd {
      margin-bottom: 8px;
    }
    .ctable th:nth-child(3),
    .ctable td:nth-child(3),
    .ctable th:nth-child(4),
    .ctable td:nth-child(4) {
      display: none;
    }
  }
</style>
