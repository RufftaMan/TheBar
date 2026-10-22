<script lang="ts">
  import Card from './Card.svelte';
  import Icon from './Icon.svelte';
  import Logo from './Logo.svelte';
  import Status from './Status.svelte';
  import { app, composeAction } from '../lib/store.svelte';
  import { dissolveGroup, renameGroup, toggleCollapse } from '../lib/arrange';
  import { itemOrder, markSeen } from '../lib/actions';
  import { draggable, dropClass } from '../lib/dnd';
  import type { View } from '../lib/layout';
  import type { GroupView } from '../lib/types';

  let { group, view, highlight = '' }: { group: GroupView; view: View; highlight?: string } = $props();

  let menu = $state(false);
  const cardIds = $derived(group.cards.map((c) => 'c:' + c.key));
  let accents = $state<Record<string, string>>({}); // logo colors for the collapsed tiles
  const collapsed = $derived(group.collapsed);
  const canDrag = $derived(!app.query.trim());

  // renaming: the pencil opens an input; a freshly created group opens it right away
  const editing = $derived(app.renaming === group.id);
  let draft = $state('');
  $effect(() => {
    if (editing) draft = group.name;
  });
  function focusSelect(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
  function saveName() {
    if (!editing) return;
    const name = draft.trim();
    if (name && name !== group.name) renameGroup(group.id, name);
    app.renaming = null;
  }
  const updatable = $derived(
    group.project
      ? [...new Set(group.cards.flatMap((c) => c.all).filter((c) => c.project === group.project && (c.update === 'available' || c.update === 'recreate')).map((c) => c.service!))]
      : [],
  );
  const drift = $derived(group.project ? [...new Set(group.cards.flatMap((c) => c.all).filter((c) => c.drift && c.project === group.project).map((c) => c.service!))] : []);

  async function stack(action: 'start' | 'stop' | 'restart' | 'update' | 'apply') {
    menu = false;
    if (!group.project) return;
    const words = {
      start: ['Start', 'Starts every service in the stack.'],
      stop: ['Stop', 'Stops every container in the stack.'],
      restart: ['Restart', 'Restarts every container in the stack.'],
      update: ['Pull & recreate', `Pulls newer images and recreates: ${updatable.join(', ')}.`],
      apply: ['Apply config', `Recreates ${drift.join(', ')} with the changed compose file.`],
    }[action];
    if (action !== 'start' && !(await app.ask(`${words[0]} ${group.name}?`, words[1], words[0], action === 'stop'))) return;
    const services = action === 'update' ? updatable : action === 'apply' ? drift : [];
    composeAction(group.project, action === 'start' ? 'up' : action, services);
  }

  async function ungroup() {
    menu = false;
    if (await app.ask(`Ungroup ${group.name}?`, 'The cards stay where they are, just without the group box around them.', 'Ungroup')) {
      dissolveGroup(view, group.id);
    }
  }
</script>

<section class="group" class:collapsed>
  <header use:draggable={{ id: 'g:' + group.id, list: 'top', kind: 'group', title: group.name, icon: group.cards[0]?.icon || '', disabled: !canDrag || editing }}>
    {#if editing}
      <input
        class="rename"
        bind:value={draft}
        use:focusSelect
        aria-label="Group name"
        onkeydown={(e) => {
          if (e.key === 'Enter') saveName();
          if (e.key === 'Escape') app.renaming = null;
        }}
        onblur={saveName} />
    {:else}
      <button class="title" onclick={() => toggleCollapse(group.id, !group.collapsed)} aria-expanded={!collapsed}>
        <span class="chev" class:open={!collapsed}><Icon name="chevronRight" size={16} /></span>
        <h2>{group.name}</h2>
      </button>
      <button class="btn sm icon ghost pencil" title="Rename group" aria-label="Rename {group.name}" onclick={() => (app.renaming = group.id)}>
        <Icon name="edit" size={13} />
      </button>
    {/if}
    <Status status={group.status} />
    {#if updatable.length}
      <span class="upd" title="Use the ⋯ menu to update the whole stack, or open a card">
        <Icon name="download" size={12} stroke={2.4} />{updatable.length} update{updatable.length === 1 ? '' : 's'}
      </span>
    {/if}
    {#if drift.length}<span class="badge drift" title="Config changed"><Icon name="file" size={11} stroke={2.5} />{drift.length}</span>{/if}
    {#if collapsed}
      <div class="minis">
        {#each group.cards as c (c.key)}
          {@const drag = { id: 'c:' + c.key, list: 'g:' + group.id, kind: 'card' as const, title: c.title, icon: c.icon, disabled: !canDrag }}
          {#if c.url}
            <a
              href={c.url}
              target="_blank"
              rel="noopener noreferrer"
              draggable="false"
              title="Open {c.title}"
              class="mini {dropClass('c:' + c.key)}"
              class:off={c.status.level === 'stopped'}
              data-mini-group={group.id}
              style="--accent:{accents[c.key] || 'var(--brand)'}"
              use:draggable={drag}
              onclick={() => markSeen(c.key)}>
              <Logo src={c.icon} name={c.title} size={38} bind:accent={accents[c.key]} />
            </a>
          {:else}
            <span
              class="mini nolink {dropClass('c:' + c.key)}"
              class:off={c.status.level === 'stopped'}
              title={c.title}
              data-mini-group={group.id}
              style="--accent:{accents[c.key] || 'var(--brand)'}"
              use:draggable={drag}>
              <Logo src={c.icon} name={c.title} size={38} bind:accent={accents[c.key]} />
            </span>
          {/if}
        {/each}
      </div>
    {/if}
    <span class="spacer"></span>
    {#if group.project}
      <div class="menuwrap">
        <button class="btn sm icon ghost" aria-label="Stack actions" title="Stack actions" onclick={() => (menu = !menu)}><Icon name="more" /></button>
        {#if menu}
          <button class="scrim" aria-label="Close menu" onclick={() => (menu = false)}></button>
          <div class="menu" role="menu">
            <button role="menuitem" onclick={() => stack('start')}><Icon name="play" size={14} />Start all</button>
            <button role="menuitem" onclick={() => stack('restart')}><Icon name="restart" size={14} />Restart all</button>
            <button role="menuitem" onclick={() => stack('stop')}><Icon name="stop" size={14} />Stop all</button>
            {#if updatable.length}<button role="menuitem" onclick={() => stack('update')}><Icon name="download" size={14} />Pull & recreate ({updatable.length})</button>{/if}
            {#if drift.length}<button role="menuitem" onclick={() => stack('apply')}><Icon name="file" size={14} />Apply config ({drift.length})</button>{/if}
            <hr />
            <button role="menuitem" onclick={() => ((menu = false), (app.modal = { kind: 'compose', project: group.project! }))}><Icon name="edit" size={14} />Edit compose file</button>
            <button role="menuitem" onclick={ungroup}><Icon name="x" size={14} />Ungroup</button>
          </div>
        {/if}
      </div>
    {:else}
      <button class="btn sm icon ghost" aria-label="Ungroup" title="Ungroup" onclick={ungroup}><Icon name="x" size={14} /></button>
    {/if}
  </header>

  {#if !collapsed}
    <div class="grid" data-list={'g:' + group.id}>
      {#each group.cards as c, i (c.key)}
        <div
          class="item {dropClass('c:' + c.key)}"
          class:wide={app.expanded === c.key}
          data-id={'c:' + c.key}
          style:order={itemOrder('c:' + c.key, i, cardIds)}>
          <Card card={c} highlight={highlight === c.key} list={'g:' + group.id} />
        </div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .group {
    background: rgba(255, 255, 255, 0.015);
    border: 1px solid var(--border);
    border-radius: calc(var(--radius) + 4px);
    padding: 10px 12px 12px;
    min-width: 0;
  }
  .group.collapsed {
    padding-bottom: 10px;
  }
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 32px;
    flex-wrap: wrap;
  }
  .title {
    display: flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    padding: 2px 4px 2px 0;
    cursor: pointer;
    border-radius: 6px;
  }
  .chev {
    display: grid;
    color: var(--text-3);
    transition: transform 0.15s;
  }
  .chev.open {
    transform: rotate(90deg);
  }
  h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 650;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: var(--text-2);
  }
  .title:hover h2 {
    color: var(--text);
  }
  .upd {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    font-weight: 500;
    color: #8fbcff;
    white-space: nowrap;
  }
  .spacer {
    flex: 1;
  }
  .rename {
    width: 240px;
    height: 30px;
    font-weight: 600;
  }
  header {
    cursor: grab;
  }
  header button,
  header input,
  header a {
    cursor: pointer;
  }
  header input {
    cursor: text;
  }
  .pencil {
    margin-left: -6px;
    color: var(--text-3);
    opacity: 0.6;
  }
  .group:hover .pencil,
  .pencil:focus-visible {
    opacity: 1;
  }
  .minis {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    margin-left: 4px;
  }
  /* same size and look as the logo button on a card */
  .mini {
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
  a.mini:hover {
    transform: translateY(-1px) scale(1.04);
    box-shadow: 0 6px 18px color-mix(in oklab, var(--accent) 30%, transparent);
  }
  .mini.off {
    filter: grayscale(0.9) brightness(0.7);
  }
  @media (max-width: 700px) {
    /* tiles get their own row so the menu stays up in the header */
    .minis {
      order: 10;
      width: 100%;
      margin-left: 0;
    }
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
    gap: 12px;
    margin-top: 10px;
    min-height: 40px;
  }
  .item {
    min-width: 0;
  }
  .item.wide {
    grid-column: 1 / -1;
  }
  .menuwrap {
    position: relative;
  }
  .scrim {
    position: fixed;
    inset: 0;
    background: transparent;
    border: none;
    z-index: 30;
    cursor: default;
  }
  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 31;
    min-width: 220px;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow);
    padding: 4px;
  }
  .menu button {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 9px;
    border: none;
    background: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
    font-size: 13px;
  }
  .menu button:hover {
    background: var(--surface-3);
  }
  .menu hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }
</style>
