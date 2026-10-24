<script lang="ts">
  import Icon from './Icon.svelte';
  import Toggle from './Toggle.svelte';
  import { app } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import { buildView } from '../lib/layout';
  import type { CardPrefs, CardView } from '../lib/types';

  let { card }: { card: CardView } = $props();

  const prefs = $derived<CardPrefs>(app.layout.cards[card.key] || {});
  const suggestion = $derived(card.main.app && !card.main.app.helper ? card.main.app.nickname || '' : '');

  function set(patch: Partial<CardPrefs>) {
    app.updateLayout((l) => {
      const cur = { ...(l.cards[card.key] || {}), ...patch };
      for (const k of Object.keys(cur) as (keyof CardPrefs)[]) if (cur[k] === undefined) delete cur[k];
      l.cards[card.key] = cur;
    });
  }

  // other cards this one could fold into
  const targets = $derived.by(() => {
    const v = buildView(app.containers, app.layout, location.hostname);
    return [...v.cards.values()].filter((c) => c.key !== card.key).sort((a, b) => a.title.localeCompare(b.title));
  });
  const parentValue = $derived(prefs.parent === undefined ? '__auto' : prefs.parent === '' ? '__own' : prefs.parent);

  // icon picker
  let iconQuery = $state('');
  let iconResults = $state<{ slug: string; url: string }[]>([]);
  let iconTimer: ReturnType<typeof setTimeout>;
  function searchIcons() {
    clearTimeout(iconTimer);
    iconTimer = setTimeout(async () => {
      if (!iconQuery.trim()) {
        iconResults = [];
        return;
      }
      iconResults = await api.get(`/api/icons?q=${encodeURIComponent(iconQuery)}`);
    }, 200);
  }
</script>

<div class="form">
  <label>
    <span class="lbl">Nickname</span>
    <input
      value={prefs.nickname ?? suggestion}
      placeholder={card.appName}
      oninput={(e) => set({ nickname: (e.currentTarget as HTMLInputElement).value })} />
    <span class="hint">
      What it does, in your words. Shown big on the card; the real name stays underneath.
      {#if prefs.nickname !== undefined && suggestion}
        <button class="linkbtn" onclick={() => set({ nickname: undefined })}>Use suggestion “{suggestion}”</button>
      {/if}
    </span>
  </label>

  <label>
    <span class="lbl">Note</span>
    <input value={prefs.note || ''} placeholder="e.g. feeds Radarr and Sonarr" oninput={(e) => set({ note: (e.currentTarget as HTMLInputElement).value || undefined })} />
  </label>

  <label>
    <span class="lbl">Link</span>
    <input
      value={prefs.url || ''}
      placeholder={card.url || 'http://…'}
      oninput={(e) => set({ url: (e.currentTarget as HTMLInputElement).value.trim() || undefined })} />
    <span class="hint">Leave empty to detect it automatically{card.url ? ` (currently ${card.url})` : ''}.</span>
  </label>

  <div class="field">
    <span class="lbl">Logo</span>
    <div class="iconrow">
      <input
        placeholder="Search logos, e.g. “plex”"
        bind:value={iconQuery}
        oninput={searchIcons}
        onkeydown={(e) => {
          if (e.key === 'Enter' && /^https?:\/\//.test(iconQuery.trim())) {
            set({ icon: iconQuery.trim() });
            iconQuery = '';
            iconResults = [];
          }
        }} />
      {#if prefs.icon}
        <button class="btn sm" onclick={() => set({ icon: undefined })}><Icon name="undo" size={12} />Automatic</button>
      {/if}
    </div>
    {#if iconResults.length}
      <div class="icons">
        {#each iconResults as r (r.slug)}
          <button class="ic" class:sel={prefs.icon === r.url} title={r.slug} onclick={() => set({ icon: r.url })}>
            <img src={r.url} alt={r.slug} loading="lazy" />
          </button>
        {/each}
      </div>
    {/if}
    <span class="hint">You can also paste an image URL into the search box and press Enter.</span>
  </div>

  <label>
    <span class="lbl">Placement</span>
    <select
      value={parentValue}
      onchange={(e) => {
        const v = (e.currentTarget as HTMLSelectElement).value;
        set({ parent: v === '__auto' ? undefined : v === '__own' ? '' : v });
      }}>
      <option value="__auto">Automatic</option>
      <option value="__own">Always its own card</option>
      <optgroup label="Fold into…">
        {#each targets as t (t.key)}
          <option value={t.key}>{t.title} ({t.subtitle})</option>
        {/each}
      </optgroup>
    </select>
    <span class="hint">Folding puts this container inside another card, like a database inside its app.</span>
  </label>

  <label class="switchrow">
    <span class="txt">
      <span class="lbl">Hide this card</span>
      <span class="hint">It moves to the “Hidden” list at the bottom.</span>
    </span>
    <Toggle
      checked={!!prefs.hidden}
      onchange={(v) => {
        set({ hidden: v || undefined });
        if (v && app.expanded === card.key) app.expanded = null; // the card leaves the grid: close it
      }} />
  </label>
</div>

<style>
  .form {
    display: grid;
    gap: 16px;
    max-width: 620px;
  }
  label,
  .field {
    display: grid;
    gap: 5px;
  }
  .lbl {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-2);
  }
  .hint {
    font-size: 12px;
    color: var(--text-3);
  }
  .linkbtn {
    background: none;
    border: none;
    color: var(--brand);
    cursor: pointer;
    padding: 0;
    font-size: 12px;
    text-decoration: underline;
  }
  .iconrow {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .iconrow input {
    flex: 1;
  }
  .icons {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(44px, 1fr));
    gap: 6px;
    max-height: 200px;
    overflow: auto;
    padding: 4px;
    background: var(--surface-2);
    border-radius: var(--radius-sm);
  }
  .ic {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 6px;
    cursor: pointer;
    aspect-ratio: 1;
  }
  .ic:hover,
  .ic.sel {
    border-color: var(--brand);
  }
  .ic img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .switchrow {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    cursor: pointer;
  }
  .switchrow .txt {
    display: grid;
    gap: 2px;
  }
</style>
