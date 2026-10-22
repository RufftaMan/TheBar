<script lang="ts">
  import { api } from '../lib/api';

  let password = $state('');
  let repeat = $state('');
  let error = $state('');
  let busy = $state(false);

  const MIN = 8;
  const tooShort = $derived(password.length > 0 && password.length < MIN);
  const mismatch = $derived(repeat.length > 0 && repeat !== password);
  const ok = $derived(password.length >= MIN && password === repeat);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!ok) return;
    busy = true;
    error = '';
    try {
      await api.post('/api/setup', { password });
      location.reload(); // the server logged us in
    } catch (err) {
      error = (err as Error).message;
      busy = false;
    }
  }
</script>

<main class="login">
  <form onsubmit={submit}>
    <img src="/favicon.svg" alt="" width="56" height="56" />
    <h1>Welcome to The<b>Bar</b></h1>
    <p class="muted">Choose a password to get started. You'll use it to log in from now on.</p>

    <!-- svelte-ignore a11y_autofocus -->
    <input type="password" bind:value={password} placeholder="Password (at least {MIN} characters)" autocomplete="new-password" autofocus aria-label="New password" />
    <input type="password" bind:value={repeat} placeholder="Repeat the password" autocomplete="new-password" aria-label="Repeat the password" />
    <div class="hint" class:bad={tooShort || mismatch}>
      {#if tooShort}
        {MIN - password.length} more character{MIN - password.length === 1 ? '' : 's'} needed
      {:else if mismatch}
        The two passwords don't match
      {:else}
        A long phrase is better than a short word.
      {/if}
    </div>
    {#if error}<div class="err">{error}</div>{/if}
    <button class="btn primary" disabled={busy || !ok}>{busy ? 'Saving…' : 'Set password & enter'}</button>

    <p class="fine muted">
      Whoever opens this page first chooses the password, so this only works from your local network.
      TheBar can start, stop and edit every container on this host. Keep it off the internet.
    </p>
  </form>
</main>

<style>
  .login {
    min-height: 100vh;
    display: grid;
    place-items: center;
    padding: var(--gutter);
  }
  form {
    width: min(380px, 100%);
    display: grid;
    gap: 12px;
    justify-items: center;
    text-align: center;
  }
  h1 {
    margin: 0;
    font-weight: 500;
    font-size: 26px;
    letter-spacing: -0.02em;
  }
  h1 b {
    color: var(--brand);
  }
  p {
    margin: -4px 0 6px;
  }
  input,
  button {
    width: 100%;
    height: 40px;
    justify-content: center;
  }
  .hint {
    font-size: 12.5px;
    color: var(--text-3);
    min-height: 18px;
  }
  .hint.bad {
    color: #f19a9a;
  }
  .err {
    color: #f19a9a;
    font-size: 13px;
  }
  .fine {
    font-size: 12px;
    margin-top: 10px;
  }
</style>
