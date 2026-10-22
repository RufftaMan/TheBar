<script lang="ts">
  import Icon from './Icon.svelte';
  import { api, ApiError } from '../lib/api';
  import { app, composeAction } from '../lib/store.svelte';
  import { createEditor, serviceLines, type Editor } from '../lib/editor';
  import { timeAgo } from '../lib/format';
  import { onDestroy, untrack } from 'svelte';

  let { project, service }: { project: string; service?: string } = $props();

  type FileDoc = { path: string; kind: 'compose' | 'env'; content: string; mtime: number };
  let files = $state<FileDoc[]>([]);
  let drafts = $state<Record<string, string>>({});
  let active = $state('');
  let loading = $state(true);
  let loadError = $state('');
  let saving = $state(false);
  let error = $state('');
  let drift = $state<string[]>([]);
  let backups = $state<{ name: string; time: number }[]>([]);
  let showBackups = $state(false);
  let host = $state<HTMLDivElement>();
  let ed: Editor | null = null;

  const current = $derived(files.find((f) => f.path === active));
  const dirty = $derived(!!current && drafts[current.path] !== undefined && drafts[current.path] !== current.content);
  const services = $derived(current?.kind === 'compose' ? serviceLines(drafts[current.path] ?? current.content) : {});

  async function load() {
    loading = true;
    loadError = '';
    try {
      const res = await api.get<{ files: FileDoc[]; drift: string[] | null }>(`/api/compose/${encodeURIComponent(project)}`);
      files = res.files;
      drift = res.drift || [];
      drafts = {};
      if (!files.some((f) => f.path === active)) active = files[0]?.path || '';
    } catch (e) {
      loadError = (e as Error).message;
    }
    loading = false;
  }
  load();

  // (re)create the editor when the active file changes or files are reloaded
  $effect(() => {
    const path = active;
    const el = host;
    files; // reloads replace the array
    if (!path || !el) return;
    untrack(() => {
      const f = files.find((x) => x.path === path);
      if (!f) return;
      ed?.destroy();
      ed = createEditor(el, drafts[f.path] ?? f.content, f.kind, (d) => (drafts[f.path] = d));
      error = '';
      if (service && f.kind === 'compose') {
        const line = serviceLines(f.content)[service];
        if (line) setTimeout(() => ed?.goto(line), 30);
      }
    });
  });
  onDestroy(() => ed?.destroy());

  async function save() {
    if (!current) return;
    saving = true;
    error = '';
    try {
      const res = await api.put<{ ok: boolean; error?: string; mtime?: number; drift?: string[]; backup?: string }>(
        `/api/compose/${encodeURIComponent(project)}/file`,
        { path: current.path, content: drafts[current.path] ?? current.content, mtime: current.mtime },
      );
      if (!res.ok) {
        error = res.error || 'Validation failed';
      } else {
        current.content = drafts[current.path] ?? current.content;
        current.mtime = res.mtime!;
        delete drafts[current.path];
        drift = res.drift || [];
        app.toast(`Saved ${current.path.split('/').pop()} (backup kept)`, 'ok');
      }
    } catch (e) {
      error = (e as Error).message;
      if (e instanceof ApiError && e.status === 409) error += ' Use “Reload” to get the current version (your edits will be lost).';
    }
    saving = false;
  }

  function revert() {
    if (!current) return;
    delete drafts[current.path];
    ed?.set(current.content);
    delete drafts[current.path];
    error = '';
  }

  async function apply() {
    const ok = await app.ask(
      'Apply config changes?',
      `Recreates ${drift.join(', ')} with the new configuration. Those services will restart.`,
      'Apply',
    );
    if (ok) composeAction(project, 'apply', drift);
  }

  async function openBackups() {
    showBackups = !showBackups;
    if (showBackups && current) {
      backups = await api.get(`/api/compose/${encodeURIComponent(project)}/backups?path=${encodeURIComponent(current.path)}`);
    }
  }
  async function loadBackup(name: string) {
    const res = await api.get<{ content: string }>(`/api/compose/${encodeURIComponent(project)}/backup/${encodeURIComponent(name)}`);
    ed?.set(res.content);
    showBackups = false;
    app.toast('Backup loaded into the editor. Save to restore it.', 'info');
  }

  function onkey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      if (dirty && !saving) save();
    }
  }

  // pick up "drift" changes reported elsewhere (e.g. after an apply job)
  $effect(() => {
    const d = new Set<string>();
    for (const c of app.containers) if (c.project === project && c.drift && c.service) d.add(c.service);
    drift = [...d].sort();
  });
</script>

<div class="ce" onkeydown={onkey} role="presentation">
  {#if loading}
    <p class="muted">Loading…</p>
  {:else if loadError}
    <p class="err">{loadError}</p>
  {:else}
    <div class="files">
      {#each files as f (f.path)}
        <button class:active={f.path === active} onclick={() => (active = f.path)} title={f.path}>
          <Icon name={f.kind === 'env' ? 'settings' : 'file'} size={13} />
          {f.path.split('/').pop()}
          {#if drafts[f.path] !== undefined && drafts[f.path] !== f.content}<span class="dot" title="Unsaved changes"></span>{/if}
        </button>
      {/each}
      <span class="path mono muted">{current?.path}</span>
    </div>

    {#if Object.keys(services).length > 1}
      <div class="jump">
        <span class="muted">Jump to</span>
        {#each Object.entries(services) as [name, line]}
          <button class="chip" class:me={name === service} class:drift={drift.includes(name)} onclick={() => ed?.goto(line)}>{name}</button>
        {/each}
      </div>
    {/if}

    <div class="editor" bind:this={host}></div>

    {#if error}
      <pre class="err">{error}</pre>
    {/if}

    {#if drift.length}
      <div class="drift">
        <Icon name="alert" size={14} />
        <span>Saved but not applied yet: <b>{drift.join(', ')}</b> still run the old configuration.</span>
        <button class="btn sm primary" onclick={apply}>Apply now</button>
      </div>
    {/if}

    <div class="actions">
      <button class="btn primary" disabled={!dirty || saving} onclick={save}>
        <Icon name="save" size={14} />{saving ? 'Validating…' : 'Validate & save'}
      </button>
      <button class="btn" disabled={!dirty} onclick={revert}><Icon name="undo" size={14} />Revert</button>
      <button class="btn ghost" onclick={load}>Reload</button>
      <div class="bk">
        <button class="btn ghost" onclick={openBackups}><Icon name="clock" size={14} />Backups</button>
        {#if showBackups}
          <div class="menu">
            {#each backups as b (b.name)}
              <button onclick={() => loadBackup(b.name)}>{new Date(b.time).toLocaleString()} <span class="muted">· {timeAgo(b.time, Date.now())}</span></button>
            {:else}
              <div class="muted pad">No backups yet. One is made every time you save.</div>
            {/each}
          </div>
        {/if}
      </div>
      <span class="muted hint">Ctrl+S saves · Ctrl+F searches · the file is checked with <code>docker compose config</code> before saving</span>
    </div>
  {/if}
</div>

<style>
  .ce {
    display: grid;
    gap: 8px;
    min-width: 0;
  }
  .files {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .files button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--surface-2);
    cursor: pointer;
    font-size: 12.5px;
  }
  .files button.active {
    border-color: var(--brand);
    color: var(--brand);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--brand);
  }
  .path {
    font-size: 11.5px;
    margin-left: 6px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .jump {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
    font-size: 12px;
  }
  .chip {
    border: 1px solid var(--border);
    background: none;
    border-radius: 999px;
    padding: 1px 9px;
    font-size: 12px;
    cursor: pointer;
    color: var(--text-2);
  }
  .chip:hover {
    border-color: var(--border-strong);
    color: var(--text);
  }
  .chip.me {
    border-color: var(--accent, var(--brand));
    color: var(--text);
  }
  .chip.drift::after {
    content: ' •';
    color: var(--warning);
  }
  .editor {
    height: min(560px, 60vh);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .editor :global(.cm-editor) {
    height: 100%;
  }
  .err {
    margin: 0;
    padding: 10px 12px;
    background: rgba(208, 59, 59, 0.1);
    border: 1px solid rgba(208, 59, 59, 0.45);
    border-radius: var(--radius-sm);
    color: #f5b5b5;
    font-family: var(--mono);
    font-size: 12px;
    white-space: pre-wrap;
  }
  .drift {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 8px 12px;
    background: rgba(250, 178, 25, 0.08);
    border: 1px solid rgba(250, 178, 25, 0.35);
    border-radius: var(--radius-sm);
    font-size: 13px;
  }
  .drift :global(svg) {
    color: var(--warning);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  .hint {
    font-size: 11.5px;
    margin-left: auto;
  }
  code {
    font-family: var(--mono);
  }
  .bk {
    position: relative;
  }
  .menu {
    position: absolute;
    bottom: calc(100% + 4px);
    left: 0;
    min-width: 280px;
    max-height: 300px;
    overflow: auto;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow);
    z-index: 20;
    padding: 4px;
  }
  .menu button {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 6px 8px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 12.5px;
  }
  .menu button:hover {
    background: var(--surface-3);
  }
  .pad {
    padding: 8px;
    font-size: 12.5px;
  }
</style>
