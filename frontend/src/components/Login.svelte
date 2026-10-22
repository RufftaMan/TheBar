<script lang="ts">
  import { api } from '../lib/api';

  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.post('/api/login', { password });
      location.reload();
    } catch (err) {
      error = (err as Error).message;
      busy = false;
    }
  }
</script>

<main class="login">
  <form onsubmit={submit}>
    <img src="/favicon.svg" alt="" width="56" height="56" />
    <h1>The<b>Bar</b></h1>
    <p class="muted">Where all your containers meet.</p>
    <!-- svelte-ignore a11y_autofocus -->
    <input type="password" bind:value={password} placeholder="Password" autocomplete="current-password" autofocus aria-label="Password" />
    {#if error}<div class="err">{error}</div>{/if}
    <button class="btn primary" disabled={busy || !password}>{busy ? 'Checking…' : 'Come in'}</button>
    <details class="forgot">
      <summary>Forgot the password?</summary>
      <p>On the server, run:</p>
      <code>docker exec -it thebar thebar passwd</code>
    </details>
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
    width: min(340px, 100%);
    display: grid;
    gap: 12px;
    justify-items: center;
    text-align: center;
  }
  h1 {
    margin: 0;
    font-weight: 500;
    font-size: 28px;
    letter-spacing: -0.02em;
  }
  h1 b {
    color: var(--brand);
  }
  p {
    margin: -6px 0 8px;
  }
  input,
  button {
    width: 100%;
    height: 40px;
    justify-content: center;
  }
  .err {
    color: #f19a9a;
    font-size: 13px;
  }
  .forgot {
    font-size: 12.5px;
    color: var(--text-3);
    margin-top: 8px;
  }
  .forgot summary {
    cursor: pointer;
  }
  .forgot p {
    margin: 8px 0 4px;
  }
  .forgot code {
    display: block;
    font-family: var(--mono);
    font-size: 12px;
    padding: 6px 8px;
    border-radius: 7px;
    background: var(--surface-2);
    color: var(--text-2);
    user-select: all;
  }
</style>
