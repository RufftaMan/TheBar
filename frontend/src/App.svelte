<script lang="ts">
  import TopBar from './components/TopBar.svelte';
  import Card from './components/Card.svelte';
  import Group from './components/Group.svelte';
  import Login from './components/Login.svelte';
  import Setup from './components/Setup.svelte';
  import Overlays from './components/Overlays.svelte';
  import Ports from './components/Ports.svelte';
  import Settings from './components/Settings.svelte';
  import Modal from './components/Modal.svelte';
  import Icon from './components/Icon.svelte';
  import { app } from './lib/store.svelte';
  import { buildView, searchCards } from './lib/layout';
  import Logo from './components/Logo.svelte';
  import { applyMove } from './lib/arrange';
  import { dropClass, setDropHandler } from './lib/dnd';
  import { itemOrder, openUrl } from './lib/actions';

  const linkHost = $derived(app.layout.settings.linkHost || location.hostname);
  const view = $derived(buildView(app.containers, app.layout, linkHost));
  const results = $derived(app.query.trim() ? searchCards(view, app.query) : []);
  let sel = $state(0);
  let searchInput = $state<HTMLInputElement>();

  $effect(() => {
    app.query;
    sel = 0;
  });

  // first visit: everything that exists now counts as "seen" (no wall of "New" badges)
  $effect(() => {
    if (app.auth && app.layout.seen.length === 0 && view.cards.size > 0) {
      const keys = [...view.cards.keys()];
      app.updateLayout((l) => {
        l.seen = keys;
      });
    }
  });

  let ComposeEditor: any = $state(null);
  $effect(() => {
    if (app.modal?.kind === 'compose' && !ComposeEditor) import('./components/ComposeEditor.svelte').then((m) => (ComposeEditor = m.default));
  });

  function searchKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      sel = Math.min(results.length - 1, sel + 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      sel = Math.max(0, sel - 1);
    } else if (e.key === 'Enter') {
      const c = results[sel];
      if (!c) return;
      e.preventDefault();
      if (c.url && !e.shiftKey) openUrl(c);
      else {
        app.query = '';
        app.expanded = c.key;
        searchInput?.blur();
        requestAnimationFrame(() => document.querySelector(`[data-id="c:${CSS.escape(c.key)}"]`)?.scrollIntoView({ block: 'center', behavior: 'smooth' }));
      }
    } else if (e.key === 'Escape') {
      app.query = '';
      searchInput?.blur();
    }
  }

  // type anywhere to search
  function globalKey(e: KeyboardEvent) {
    if (!app.auth || app.modal || app.confirm) return;
    const t = e.target as HTMLElement;
    if (t && (t.closest('input, textarea, select, [contenteditable], .cm-editor') || t.isContentEditable)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === '/') {
      e.preventDefault();
      searchInput?.focus();
      return;
    }
    if (e.key === 'Escape' && app.expanded) {
      app.expanded = null;
      return;
    }
    if (e.key.length === 1 && /\S/.test(e.key)) searchInput?.focus(); // the key then lands in the input
  }

  // While a card is open the page never gets shorter. A shorter page near the
  // bottom would make the browser clamp the scroll position, and the view would
  // jump when you switch to a shorter tab. This way the card simply grows and
  // shrinks downward from its stable top; any slack ends up below the last card.
  let mainEl = $state<HTMLElement>();
  let pageMin = $state(0);
  $effect(() => {
    if (!mainEl) return;
    if (!app.expanded) {
      pageMin = 0;
      return;
    }
    pageMin = Math.max(pageMin, mainEl.offsetHeight);
    const ro = new ResizeObserver(() => {
      const h = mainEl!.offsetHeight;
      if (h > pageMin) pageMin = h;
    });
    ro.observe(mainEl);
    return () => ro.disconnect();
  });

  const resultIds = $derived(results.map((c) => 'c:' + c.key));
  const hiddenIds = $derived(view.hidden.map((c) => 'c:' + c.key));
  const visibleTop = $derived(view.top.filter((i) => i.kind === 'card' || i.group.cards.length > 0));

  // drag & drop: drops become layout moves
  setDropHandler((d, t) => applyMove(view, d, t));
  const dropHint = $derived.by(() => {
    const t = app.drag?.target;
    if (!t) return '';
    if (t.type === 'merge') return 'New group';
    if (t.type === 'into') return 'Into ' + (view.groups.get(t.group)?.name || 'group');
    return '';
  });
  const topIds = $derived(visibleTop.map((i) => i.id));
</script>

<svelte:window onkeydown={globalKey} />

{#if !app.auth}
  {#if app.setup}
    <Setup />
  {:else}
    <Login />
  {/if}
{:else}
  <TopBar onkey={searchKey} bind:input={searchInput} />

  <main bind:this={mainEl} style:min-height={pageMin ? pageMin + 'px' : null}>
    {#if app.query.trim()}
      <div class="results-head muted">
        {#if results.length}
          <span><kbd>↵</kbd> open <b>{results[sel]?.title}</b> · <kbd>⇧↵</kbd> details · <kbd>↑↓</kbd> choose · <kbd>esc</kbd> clear</span>
        {:else}
          Nothing matches “{app.query}”.
        {/if}
      </div>
      <div class="grid">
        {#each results as c, i (c.key)}
          <div class="item" class:wide={app.expanded === c.key} data-id={'c:' + c.key} style:order={itemOrder('c:' + c.key, i, resultIds)}>
            <Card card={c} highlight={i === sel} />
          </div>
        {/each}
      </div>
    {:else}
      <div class="grid top" data-list="top">
        {#each visibleTop as it, i (it.id)}
          <div
            class="item {dropClass(it.id)}"
            class:wide={it.kind === 'group' || app.expanded === it.id.slice(2)}
            data-id={it.id}
            style:order={itemOrder(it.id, i, topIds)}>
            {#if it.kind === 'card'}
              <Card card={it.card} list="top" />
            {:else}
              <Group group={it.group} {view} />
            {/if}
          </div>
        {/each}
      </div>

      {#if view.top.length === 0}
        <div class="empty">
          <p>No containers found. Is the Docker socket mounted?</p>
        </div>
      {/if}

      {#if view.hidden.length}
        <details class="hidden">
          <summary><Icon name="eyeOff" size={14} />Hidden ({view.hidden.length})</summary>
          <div class="grid">
            {#each view.hidden as c, i (c.key)}
              <div class="item" class:wide={app.expanded === c.key} data-id={'c:' + c.key} style:order={itemOrder('c:' + c.key, i, hiddenIds)}>
                <Card card={c} />
              </div>
            {/each}
          </div>
        </details>
      {/if}
    {/if}
  </main>

  {#if app.modal?.kind === 'ports'}
    <Ports />
  {:else if app.modal?.kind === 'settings'}
    <Settings />
  {:else if app.modal?.kind === 'compose'}
    <Modal title="Compose: {app.modal.project}" wide onclose={() => (app.modal = null)}>
      {#if ComposeEditor}
        <ComposeEditor project={app.modal.project} service={app.modal.service} />
      {:else}
        <p class="muted">Loading editor…</p>
      {/if}
    </Modal>
  {/if}

  {#if app.drag}
    <div class="ghost" style="left:{app.drag.x}px;top:{app.drag.y}px" aria-hidden="true">
      <span class="ghost-logo"><Logo src={app.drag.icon} name={app.drag.title} size={34} /></span>
      <span class="ghost-title">{app.drag.title}</span>
      {#if dropHint}<span class="ghost-hint">{dropHint}</span>{/if}
    </div>
  {/if}

  <Overlays />
{/if}

<style>
  main {
    padding: 18px var(--gutter) 90px;
    max-width: 1800px;
    margin: 0 auto;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
    gap: 14px;
    align-items: start;
  }
  .item {
    min-width: 0;
  }
  .item.wide {
    grid-column: 1 / -1;
  }
  .results-head {
    font-size: 12.5px;
    margin-bottom: 12px;
  }
  .results-head b {
    color: var(--text);
  }
  kbd {
    font-family: var(--mono);
    font-size: 11px;
    border: 1px solid var(--border-strong);
    border-radius: 5px;
    padding: 0 5px;
    color: var(--text-2);
  }
  .ghost {
    position: fixed;
    z-index: 90;
    transform: translate(-50%, -60%) rotate(-2deg);
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px 8px 8px;
    border-radius: 14px;
    background: var(--surface-3);
    border: 1px solid var(--brand);
    box-shadow: 0 16px 40px rgba(0, 0, 0, 0.55);
    pointer-events: none;
    white-space: nowrap;
  }
  .ghost-logo {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 11px;
    background: var(--surface-2);
  }
  .ghost-title {
    font-weight: 650;
  }
  .ghost-hint {
    font-size: 12px;
    color: var(--brand);
    font-weight: 600;
  }
  .hidden {
    margin-top: 28px;
  }
  .hidden summary {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    color: var(--text-3);
    font-size: 13px;
    margin-bottom: 12px;
  }
  .empty {
    text-align: center;
    color: var(--text-3);
    padding: 60px 0;
  }
</style>
