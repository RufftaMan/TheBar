<script lang="ts">
  // Jobs console, toasts and the confirm dialog.
  import Icon from './Icon.svelte';
  import { app } from '../lib/store.svelte';
  import { duration } from '../lib/format';

  let openJob = $state<string | null>(null);
  const jobs = $derived([...app.jobs].reverse().slice(0, 8));
  const runningCount = $derived(app.jobs.filter((j) => j.state === 'running').length);
  const shownJob = $derived(app.jobs.find((j) => j.id === openJob) || jobs[0]);
  let out: HTMLPreElement | undefined = $state();

  $effect(() => {
    shownJob?.lines.length;
    if (out) queueMicrotask(() => out && (out.scrollTop = out.scrollHeight));
  });

  // Move focus into the dialog. Dangerous actions (Stop…) focus Cancel, so a
  // stray Enter doesn't confirm. (Plain autofocus won't steal focus from the
  // button that opened the dialog.)
  let cancelBtn = $state<HTMLButtonElement>();
  let okBtn = $state<HTMLButtonElement>();
  $effect(() => {
    const c = app.confirm;
    const target = c?.danger ? cancelBtn : okBtn;
    if (c && target) target.focus();
  });

  function answer(v: boolean) {
    app.confirm?.resolve(v);
    app.confirm = null;
  }
</script>

{#if app.jobs.length}
  <div class="jobs" class:open={app.jobsOpen}>
    <button class="jobs-head" onclick={() => (app.jobsOpen = !app.jobsOpen)}>
      <Icon name="terminal" size={14} />
      {#if runningCount}
        <span class="spin"></span>{runningCount} running
      {:else}
        Jobs
      {/if}
      <span class="chev" class:up={!app.jobsOpen}><Icon name="chevronDown" size={14} /></span>
    </button>
    {#if app.jobsOpen && shownJob}
      <div class="jobs-body">
        <div class="jlist">
          {#each jobs as j (j.id)}
            <button class="jitem" class:sel={j.id === shownJob.id} onclick={() => (openJob = j.id)}>
              <span class="st {j.state}">{j.state === 'running' ? '…' : j.state === 'ok' ? '✓' : '✕'}</span>
              <span class="jt">{j.title}</span>
              <span class="muted">{j.ended ? duration(j.ended - j.started) : ''}</span>
            </button>
          {/each}
        </div>
        <pre bind:this={out}>{shownJob.lines.join('\n') || 'Starting…'}</pre>
      </div>
    {/if}
  </div>
{/if}

<div class="toasts" aria-live="polite">
  {#each app.toasts as t (t.id)}
    <div class="toast {t.kind}">
      <Icon name={t.kind === 'error' ? 'xCircle' : t.kind === 'ok' ? 'checkCircle' : 'alert'} size={15} />
      <span>{t.text}</span>
    </div>
  {/each}
</div>

{#if app.confirm}
  <div class="scrim" role="presentation" onclick={(e) => e.target === e.currentTarget && answer(false)}>
    <div class="confirm" role="alertdialog" aria-modal="true" aria-labelledby="cf-t">
      <h3 id="cf-t">{app.confirm.title}</h3>
      {#if app.confirm.body}<p>{app.confirm.body}</p>{/if}
      <div class="btns">
        <button class="btn" bind:this={cancelBtn} onclick={() => answer(false)}>Cancel</button>
        <button class="btn {app.confirm.danger ? 'dangerfill' : 'primary'}" bind:this={okBtn} onclick={() => answer(true)}>{app.confirm.ok}</button>
      </div>
    </div>
  </div>
{/if}

<svelte:window onkeydown={(e) => app.confirm && e.key === 'Escape' && answer(false)} />

<style>
  .jobs {
    position: fixed;
    right: var(--gutter);
    bottom: var(--gutter);
    z-index: 50;
    width: min(560px, calc(100vw - 2 * var(--gutter)));
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    pointer-events: none;
  }
  .jobs > * {
    pointer-events: auto;
  }
  .jobs-head {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 34px;
    padding: 0 12px;
    border-radius: 10px;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow);
    cursor: pointer;
    font-size: 13px;
  }
  .chev {
    display: grid;
    transition: transform 0.15s;
  }
  .chev.up {
    transform: rotate(180deg);
  }
  .jobs-body {
    order: -1;
    margin-bottom: 8px;
    width: 100%;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .jlist {
    max-height: 130px;
    overflow: auto;
    border-bottom: 1px solid var(--border);
    padding: 4px;
  }
  .jitem {
    display: flex;
    gap: 8px;
    align-items: center;
    width: 100%;
    border: none;
    background: none;
    padding: 5px 8px;
    border-radius: 6px;
    text-align: left;
    cursor: pointer;
    font-size: 12.5px;
  }
  .jitem.sel,
  .jitem:hover {
    background: var(--surface-2);
  }
  .jt {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .st {
    width: 14px;
    text-align: center;
    font-weight: 700;
  }
  .st.ok {
    color: var(--good);
  }
  .st.failed {
    color: var(--critical);
  }
  .st.running {
    color: var(--warning);
  }
  pre {
    margin: 0;
    height: 220px;
    overflow: auto;
    padding: 10px 12px;
    background: #090b0e;
    font-family: var(--mono);
    font-size: 11.5px;
    line-height: 1.5;
    color: #cfd4dc;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .spin {
    width: 12px;
    height: 12px;
    border: 2px solid var(--warning);
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .toasts {
    position: fixed;
    left: 50%;
    bottom: var(--gutter);
    transform: translateX(-50%);
    z-index: 70;
    display: grid;
    gap: 8px;
    width: min(480px, calc(100vw - 2 * var(--gutter)));
    pointer-events: none;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 10px 14px;
    border-radius: 11px;
    background: var(--surface-3);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow);
    font-size: 13px;
    animation: up 0.18s ease-out;
  }
  .toast.ok :global(svg) {
    color: var(--good);
  }
  .toast.error {
    border-color: rgba(208, 59, 59, 0.6);
  }
  .toast.error :global(svg) {
    color: var(--critical);
  }
  .toast.info :global(svg) {
    color: var(--brand);
  }
  @keyframes up {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 80;
    background: rgba(5, 6, 8, 0.55);
    display: grid;
    place-items: center;
    padding: var(--gutter);
  }
  .confirm {
    width: min(440px, 100%);
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 18px;
    box-shadow: 0 30px 80px rgba(0, 0, 0, 0.5);
  }
  h3 {
    margin: 0 0 6px;
    font-size: 16px;
  }
  p {
    margin: 0;
    color: var(--text-2);
    font-size: 13.5px;
    overflow-wrap: anywhere;
  }
  .btns {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 18px;
  }
  .dangerfill {
    background: var(--critical);
    border-color: var(--critical);
    color: #fff;
    font-weight: 600;
  }
</style>
