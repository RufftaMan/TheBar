<script lang="ts">
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import Toggle from './Toggle.svelte';
  import { api } from '../lib/api';
  import { app } from '../lib/store.svelte';
  import { timeAgo } from '../lib/format';

  const s = $derived(app.layout.settings);

  function set<K extends keyof typeof s>(k: K, v: (typeof s)[K]) {
    app.updateLayout((l) => {
      l.settings[k] = v;
    });
  }
  async function checkUpdates() {
    await api.post('/api/updates/check');
    app.toast('Checking for image updates in the background…');
  }
  // change password
  let pwCurrent = $state('');
  let pwNew = $state('');
  let pwRepeat = $state('');
  let pwBusy = $state(false);
  let pwError = $state('');
  const pwTooShort = $derived(pwNew.length > 0 && pwNew.length < 8);
  const pwMismatch = $derived(pwRepeat.length > 0 && pwRepeat !== pwNew);
  const pwOk = $derived(pwCurrent.length > 0 && pwNew.length >= 8 && pwNew === pwRepeat);

  async function changePassword(e: SubmitEvent) {
    e.preventDefault();
    if (!pwOk) return;
    pwBusy = true;
    pwError = '';
    try {
      await api.post('/api/password', { current: pwCurrent, next: pwNew });
      pwCurrent = pwNew = pwRepeat = '';
      app.toast('Password changed. Other devices were logged out.', 'ok');
    } catch (err) {
      pwError = (err as Error).message;
    }
    pwBusy = false;
  }

  async function logout() {
    await api.post('/api/logout');
    location.reload();
  }
  function restoreGroups() {
    app.updateLayout((l) => {
      l.dissolved = [];
    });
  }
  async function resetLayout() {
    if (!(await app.ask('Reset the layout?', 'Order, groups, collapsed state and “new” markers go back to the defaults. Nicknames, notes, links and hidden cards are kept.', 'Reset', true))) return;
    app.updateLayout((l) => {
      l.top = [];
      l.groups = {};
      l.dissolved = [];
    });
  }
</script>

<Modal title="Settings" onclose={() => (app.modal = null)}>
  <div class="form">
    <label class="switchrow">
      <span class="txt">
        <span class="lbl">Open apps through their proxy domain when there is one</span>
        <span class="hint">Domains are read from Nginx Proxy Manager. Off: always use host:port.</span>
      </span>
      <Toggle checked={s.preferDomain} onchange={(v) => set('preferDomain', v)} />
    </label>

    <label>
      <span class="lbl">Host name for links</span>
      <input value={s.linkHost} placeholder={location.hostname} onchange={(e) => set('linkHost', (e.currentTarget as HTMLInputElement).value.trim())} />
      <span class="hint">Leave empty to use whatever address you opened TheBar with ({location.hostname}).</span>
    </label>

    <label>
      <span class="lbl">Grafana link template</span>
      <input
        value={s.grafana}
        placeholder="automatic: Grafana Explore with the container's CPU"
        onchange={(e) => set('grafana', (e.currentTarget as HTMLInputElement).value.trim())} />
      <span class="hint">
        Use <code>{'{grafana}'}</code>, <code>{'{name}'}</code>, <code>{'{project}'}</code>, <code>{'{service}'}</code>. Example:
        <code>{'{grafana}/d/abc123/docker?var-container={name}'}</code>
      </span>
    </label>

    <div class="field">
      <span class="lbl">Image updates</span>
      <div class="row">
        <button class="btn" onclick={checkUpdates}><Icon name="download" size={14} />Check now</button>
        <span class="muted">Last check: {timeAgo(app.updatesChecked, app.now)}. Runs automatically every 12 hours.</span>
      </div>
    </div>

    <div class="field">
      <span class="lbl">Layout</span>
      <div class="row">
        {#if app.layout.dissolved.length}
          <button class="btn" onclick={restoreGroups}><Icon name="layers" size={14} />Restore automatic groups ({app.layout.dissolved.join(', ')})</button>
        {/if}
        <button class="btn danger" onclick={resetLayout}><Icon name="undo" size={14} />Reset layout</button>
      </div>
    </div>

    {#if app.authEnabled}
      <div class="field">
        <span class="lbl">Password</span>
        {#if app.passwordManaged}
          <span class="hint">The password is set by <code>THEBAR_PASSWORD</code> in TheBar's environment. Change it there and restart TheBar.</span>
        {:else}
          <form class="pw" onsubmit={changePassword}>
            <input type="password" bind:value={pwCurrent} placeholder="Current password" autocomplete="current-password" aria-label="Current password" />
            <input type="password" bind:value={pwNew} placeholder="New password (at least 8 characters)" autocomplete="new-password" aria-label="New password" />
            <input type="password" bind:value={pwRepeat} placeholder="Repeat the new password" autocomplete="new-password" aria-label="Repeat the new password" />
            {#if pwTooShort}
              <span class="hint bad">{8 - pwNew.length} more character{8 - pwNew.length === 1 ? '' : 's'} needed</span>
            {:else if pwMismatch}
              <span class="hint bad">The two passwords don't match</span>
            {/if}
            {#if pwError}<span class="hint bad">{pwError}</span>{/if}
            <div class="row">
              <button class="btn" disabled={!pwOk || pwBusy}><Icon name="save" size={14} />{pwBusy ? 'Saving…' : 'Change password'}</button>
              <span class="muted">Other devices will be logged out.</span>
            </div>
          </form>
          <span class="hint">Forgot it? On the server run <code>docker exec -it thebar thebar passwd</code>.</span>
        {/if}
        <div class="row"><button class="btn" onclick={logout}><Icon name="logout" size={14} />Log out</button></div>
      </div>
    {/if}

    <p class="muted ver">TheBar {app.version}</p>
  </div>
</Modal>

<style>
  .form {
    display: grid;
    gap: 18px;
  }
  label,
  .field {
    display: grid;
    gap: 5px;
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
  .lbl {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-2);
  }
  .hint {
    font-size: 12px;
    color: var(--text-3);
  }
  code {
    font-family: var(--mono);
    font-size: 11.5px;
    background: var(--surface-2);
    padding: 1px 4px;
    border-radius: 4px;
  }
  .row {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 12.5px;
  }
  .pw {
    display: grid;
    gap: 8px;
    max-width: 380px;
  }
  .hint.bad {
    color: #f19a9a;
  }
  .ver {
    font-size: 11px;
    margin: 0;
  }
</style>
