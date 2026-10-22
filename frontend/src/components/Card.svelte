<script lang="ts">
  import Logo from './Logo.svelte';
  import Icon from './Icon.svelte';
  import Status from './Status.svelte';
  import Sparkline from './Sparkline.svelte';
  import CardDetail from './CardDetail.svelte';
  import { app } from '../lib/store.svelte';
  import { aggregate, last } from '../lib/stats';
  import { bytes, cores, pct, rate, since, timeAgo } from '../lib/format';
  import { markSeen, toggleCard } from '../lib/actions';
  import { draggable } from '../lib/dnd';
  import type { CardView } from '../lib/types';

  // list: where the card lives ("top" or "g:<groupId>"); empty = not draggable (search results, hidden list)
  let { card, highlight = false, list = '' }: { card: CardView; highlight?: boolean; list?: string } = $props();

  let accent = $state('');
  const expanded = $derived(app.expanded === card.key);
  const ids = $derived(card.all.filter((c) => c.state === 'running').map((c) => c.id));
  const agg = $derived(aggregate(app.history, ids));
  const disk = $derived(card.all.reduce((s, c) => s + (c.sizeRw || 0) + (c.volSize || 0), 0));
  const running = $derived(card.status.running > 0);
  const uptime = $derived.by(() => {
    const m = card.main;
    if (m.state === 'running') return 'Up ' + since(m.startedAt, app.now);
    if (m.finishedAt) return 'Stopped ' + timeAgo(Date.parse(m.finishedAt), app.now);
    return m.status;
  });
  const linkText = $derived.by(() => {
    if (!card.url) return '';
    try {
      const u = new URL(card.url);
      return u.hostname === (app.layout.settings.linkHost || location.hostname) ? ':' + u.port : u.host;
    } catch {
      return card.url;
    }
  });

  let el: HTMLElement;
  function toggle() {
    toggleCard(card.key, el?.closest('.item'));
  }
  function keydown(e: KeyboardEvent) {
    if (e.target !== e.currentTarget) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      toggle();
    }
  }
</script>

<article
  bind:this={el}
  class="card"
  class:expanded
  class:stopped={!running}
  class:highlight
  class:critical={card.status.level === 'critical'}
  data-id={'c:' + card.key}
  style="--accent:{accent || 'var(--brand)'}">
  <div
    class="compact"
    role="button"
    tabindex="0"
    aria-expanded={expanded}
    onclick={toggle}
    onkeydown={keydown}
    use:draggable={{ id: 'c:' + card.key, list, kind: 'card', title: card.title, icon: card.icon, disabled: !list || expanded || !!app.query.trim() }}>
    <div class="head">
      {#if card.url}
        <a
          class="logo"
          draggable="false"
          href={card.url}
          target="_blank"
          rel="noopener noreferrer"
          title="Open {card.title} ({card.url})"
          onclick={(e) => {
            e.stopPropagation();
            markSeen(card.key);
          }}>
          <Logo src={card.icon} name={card.title} size={expanded ? 44 : 38} bind:accent />
          <span class="open"><Icon name="external" size={11} stroke={2.5} /></span>
        </a>
      {:else}
        <span class="logo nolink" title="No web interface">
          <Logo src={card.icon} name={card.title} size={expanded ? 44 : 38} bind:accent />
        </span>
      {/if}
      <div class="names">
        <h3 title={card.title}>{card.title}{#if card.series}<span class="muted"> ×{card.series}</span>{/if}</h3>
        <div class="sub" title={card.subtitle}>{card.subtitle}</div>
      </div>
      <div class="state">
        <Status status={card.status} />
        {#if card.update || card.recreate}
          <span
            class="upd"
            title={card.update
              ? 'A newer image is available. Open the card to update.'
              : 'A newer image is already downloaded, but the container still runs the old one. Open the card to update.'}>
            <Icon name="download" size={12} stroke={2.4} />Update available
          </span>
        {/if}
      </div>
    </div>

    {#if card.isNew || card.drift || card.hidden}
      <div class="badges">
        {#if card.isNew}<span class="badge new">New</span>{/if}
        {#if card.drift}<span class="badge drift" title="The compose file changed since this container was created"><Icon name="file" size={11} stroke={2.5} />Config changed</span>{/if}
        {#if card.hidden}<span class="badge"><Icon name="eyeOff" size={11} />Hidden</span>{/if}
      </div>
    {/if}

    {#if !expanded}
      {#if running}
        <div class="stats">
          <div class="stat cpu" title="Share of the whole machine: {cores(last(agg.cpu), app.host?.cores)}">
            <div class="k">CPU <b class="num">{pct(last(agg.cpu))}</b></div>
            <Sparkline values={agg.cpu} color={accent || 'var(--brand)'} min={1} height={22} label="CPU trend" format={(v) => `${pct(v)} · ${cores(v, app.host?.cores)}`} />
          </div>
          <div class="stat">
            <div class="k">RAM</div>
            <div class="val num">{bytes(last(agg.mem))}</div>
          </div>
          <div class="stat">
            <div class="k">NET</div>
            <div class="val num net">
              {#if agg.hasNet}
                <span title="Download"><Icon name="chevronDown" size={11} stroke={3} />{rate(last(agg.rx))}</span>
                <span title="Upload"><Icon name="chevronDown" size={11} stroke={3} />{rate(last(agg.tx))}</span>
              {:else}
                <span class="muted" title="Uses host networking: traffic isn't measured per container">host</span>
              {/if}
            </div>
          </div>
          <div class="stat">
            <div class="k">DISK</div>
            <div class="val num" title="Writable layer + named volumes (bind mounts not counted)">{disk ? bytes(disk) : '–'}</div>
          </div>
        </div>
      {/if}
      <div class="foot">
        <span class="muted"><Icon name="clock" size={12} />{uptime}</span>
        {#if card.note}<span class="note" title={card.note}>{card.note}</span>{/if}
        {#if linkText}<span class="muted link">{linkText}</span>{/if}
      </div>
    {/if}
  </div>

  {#if expanded}
    <CardDetail {card} {accent} />
  {/if}
</article>

<style>
  .card {
    position: relative;
    background: linear-gradient(180deg, color-mix(in oklab, var(--accent) 7%, var(--surface)) 0%, var(--surface) 60%);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    min-width: 0;
    transition:
      border-color 0.15s,
      box-shadow 0.15s,
      transform 0.15s;
  }
  .card::before {
    content: '';
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    border-radius: var(--radius) var(--radius) 0 0;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
    opacity: 0.55;
  }
  .card:hover {
    border-color: color-mix(in oklab, var(--accent) 35%, var(--border));
  }
  .card.highlight {
    border-color: var(--brand);
    box-shadow: 0 0 0 3px var(--brand-soft);
  }
  .card.critical {
    border-color: color-mix(in oklab, var(--critical) 55%, var(--border));
  }
  .card.stopped .logo :global(img),
  .card.stopped .logo :global(.mono-logo) {
    filter: grayscale(0.85) brightness(0.8);
  }
  .card.expanded {
    grid-column: 1 / -1;
    border-color: color-mix(in oklab, var(--accent) 40%, var(--border));
    box-shadow: var(--shadow);
  }
  .compact {
    padding: 14px 14px 12px;
    cursor: pointer;
    -webkit-touch-callout: none; /* long-press drags instead of opening a menu */
    outline-offset: -2px;
    border-radius: var(--radius);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .logo {
    position: relative;
    flex-shrink: 0;
    display: grid;
    place-items: center;
    width: 54px;
    height: 54px;
    border-radius: 14px;
    background: color-mix(in oklab, var(--accent) 12%, var(--surface-2));
    border: 1px solid color-mix(in oklab, var(--accent) 22%, var(--border));
    transition:
      transform 0.15s,
      box-shadow 0.15s;
  }
  .expanded .logo {
    width: 62px;
    height: 62px;
  }
  a.logo:hover {
    transform: translateY(-1px) scale(1.04);
    box-shadow: 0 6px 18px color-mix(in oklab, var(--accent) 30%, transparent);
  }
  .open {
    position: absolute;
    right: -5px;
    top: -5px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--surface-3);
    border: 1px solid var(--border-strong);
    color: var(--text-2);
    opacity: 0;
    transition: opacity 0.15s;
  }
  a.logo:hover .open,
  a.logo:focus-visible .open {
    opacity: 1;
  }
  .names {
    flex: 1;
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: 15.5px;
    font-weight: 650;
    letter-spacing: -0.01em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .expanded h3 {
    font-size: 18px;
  }
  .sub {
    font-size: 12px;
    color: var(--text-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 3px;
    flex-shrink: 0;
  }
  /* a label, not a button: the action lives in the expanded card */
  .upd {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11.5px;
    font-weight: 500;
    color: #8fbcff;
    white-space: nowrap;
  }
  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin-top: 10px;
  }
  .stats {
    display: grid;
    grid-template-columns: 1.5fr 1fr 1.3fr 1fr;
    gap: 10px;
    margin-top: 12px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
  }
  .stat {
    min-width: 0;
  }
  .k {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--text-3);
    margin-bottom: 3px;
    white-space: nowrap;
  }
  .k b {
    color: var(--text);
    font-weight: 600;
    letter-spacing: 0;
    font-size: 12px;
    margin-left: 3px;
  }
  .val {
    font-size: 12px;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .net {
    display: flex;
    flex-direction: column;
    line-height: 1.25;
  }
  .net span {
    display: inline-flex;
    align-items: center;
    gap: 1px;
  }
  .net span:last-child :global(svg) {
    transform: rotate(180deg);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 10px;
    font-size: 11.5px;
    min-width: 0;
  }
  .foot > span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    white-space: nowrap;
  }
  .note {
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    flex: 1;
    display: block !important;
  }
  .link {
    margin-left: auto;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
