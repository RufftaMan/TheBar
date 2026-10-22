<script lang="ts">
  import Icon from './Icon.svelte';
  import type { CardStatus } from '../lib/types';

  let { status, compact = false }: { status: CardStatus; compact?: boolean } = $props();
  const icon = $derived(
    { good: 'checkCircle', warning: 'alert', critical: 'xCircle', stopped: 'minusCircle' }[status.level],
  );
</script>

<span class="status {status.level}" class:compact title={status.label}>
  <Icon name={icon} size={compact ? 13 : 14} stroke={2.2} />
  {#if !compact}<span class="label">{status.label}</span>{/if}
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-2);
    white-space: nowrap;
    min-width: 0;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .good :global(svg) {
    color: var(--good);
  }
  .warning :global(svg) {
    color: var(--warning);
  }
  .critical :global(svg) {
    color: var(--critical);
  }
  .critical .label {
    color: #f19a9a;
  }
  .stopped :global(svg) {
    color: var(--stopped);
  }
</style>
