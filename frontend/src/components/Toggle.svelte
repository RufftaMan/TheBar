<script lang="ts">
  // On/off switch. Put it inside a <label> and clicking the label text works too.
  let {
    checked = false,
    onchange,
    label = '',
  }: { checked?: boolean; onchange?: (value: boolean) => void; label?: string } = $props();
</script>

<button type="button" role="switch" class="switch" class:on={checked} aria-checked={checked} aria-label={label || undefined} onclick={() => onchange?.(!checked)}>
  <i></i>
</button>

<style>
  .switch {
    flex-shrink: 0;
    position: relative;
    width: 38px;
    height: 22px;
    padding: 0;
    border-radius: 999px;
    border: 1px solid var(--border-strong);
    background: var(--surface-3);
    cursor: pointer;
    transition:
      background 0.15s,
      border-color 0.15s;
  }
  .switch i {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--text-2);
    transition:
      transform 0.15s,
      background 0.15s;
  }
  .switch:hover {
    border-color: #454c5a;
  }
  .switch.on {
    background: var(--brand);
    border-color: var(--brand);
  }
  .switch.on i {
    transform: translateX(16px);
    background: #1b1405;
  }
</style>
