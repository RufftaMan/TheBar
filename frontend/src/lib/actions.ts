import { tick } from 'svelte';
import { app, composeAction, containerAction } from './store.svelte';
import type { CardView, Container } from './types';

export function markSeen(key: string) {
  if (app.layout.seen.includes(key)) return;
  app.updateLayout((l) => {
    if (!l.seen.includes(key)) l.seen.push(key);
  });
}

/**
 * Open or close a card in place. The open card spans the full row; to keep it
 * from jumping, it's placed before the first card of the row it was in (the
 * row's other cards move below it), and the page is scrolled so its top edge
 * stays where it was under the mouse.
 */
export async function toggleCard(key: string, itemEl: HTMLElement | null) {
  markSeen(key);
  if (app.expanded === key) {
    app.expanded = null;
    app.expandAnchor = null;
    return;
  }
  const topBefore = itemEl?.getBoundingClientRect().top ?? 0;
  if (app.expanded) {
    // close the other card first so rows are measured in their normal layout
    app.expanded = null;
    app.expandAnchor = null;
    await tick();
  }
  let before = '';
  if (itemEl?.parentElement) {
    const row = itemEl.offsetTop;
    const first = [...itemEl.parentElement.children].find(
      (el): el is HTMLElement => el instanceof HTMLElement && el.classList.contains('item') && el.offsetTop === row,
    );
    before = first?.dataset.id || '';
  }
  app.expandAnchor = before ? { key, before } : null;
  app.expanded = key;
  await tick();
  if (itemEl) {
    const delta = itemEl.getBoundingClientRect().top - topBefore;
    if (Math.abs(delta) > 1) window.scrollBy({ top: delta, behavior: 'instant' as ScrollBehavior });
  }
}

/** CSS order for grid items, so the expanded card can sit at its row's start. */
export function itemOrder(id: string, index: number, ids: string[]): number {
  const a = app.expandAnchor;
  if (a && app.expanded === a.key && id === 'c:' + a.key) {
    const at = ids.indexOf(a.before);
    if (at >= 0 && at < index) return at * 2 - 1;
  }
  return index * 2;
}

export function openUrl(card: CardView) {
  markSeen(card.key);
  if (card.url) window.open(card.url, '_blank', 'noopener');
}

/** Card-level start/stop/restart: helpers start first and stop last. */
export async function cardAction(card: CardView, action: 'start' | 'stop' | 'restart') {
  const helpers = card.folded.flatMap((f) => f.containers);
  const own = card.own;
  if (own.some((c) => c.self) && action === 'stop') {
    app.toast("TheBar can't stop itself from here.", 'error');
    return;
  }
  let order: Container[];
  if (action === 'stop') order = [...own, ...helpers].filter((c) => c.state === 'running' || c.state === 'restarting');
  else if (action === 'start') order = [...helpers, ...own].filter((c) => c.state !== 'running');
  else order = [...helpers, ...own].filter((c) => c.state === 'running' || c.state === 'restarting');
  if (!order.length) {
    app.toast(action === 'stop' ? 'Nothing is running.' : action === 'start' ? 'Everything is already running.' : 'Nothing is running to restart.');
    return;
  }
  if (action !== 'start') {
    const ok = await app.ask(
      `${action === 'stop' ? 'Stop' : 'Restart'} ${card.title}?`,
      `This will ${action} ${order.length} container${order.length > 1 ? 's' : ''}: ${order.map((c) => c.name).join(', ')}.`,
      action === 'stop' ? 'Stop' : 'Restart',
      action === 'stop',
    );
    if (!ok) return;
  }
  app.toast(`${action === 'start' ? 'Starting' : action === 'stop' ? 'Stopping' : 'Restarting'} ${card.title}…`);
  for (const c of order) {
    try {
      await containerAction(c.id, action);
    } catch (e) {
      app.toast(`${c.name}: ${(e as Error).message}`, 'error');
      return;
    }
  }
  app.toast(`${card.title}: ${action} done`, 'ok');
}

export async function singleAction(c: Container, action: 'start' | 'stop' | 'restart') {
  if (action !== 'start') {
    const ok = await app.ask(`${action === 'stop' ? 'Stop' : 'Restart'} ${c.name}?`, '', action === 'stop' ? 'Stop' : 'Restart', action === 'stop');
    if (!ok) return;
  }
  try {
    await containerAction(c.id, action);
    app.toast(`${c.name}: ${action} done`, 'ok');
  } catch (e) {
    app.toast(`${c.name}: ${(e as Error).message}`, 'error');
  }
}

/** Pull the newest image(s) and recreate the card's compose services. */
export async function updateCard(card: CardView) {
  if (!card.project) return;
  const services = [...new Set(card.all.filter((c) => c.project === card.project && (c.update === 'available' || c.update === 'recreate')).map((c) => c.service!))];
  if (!services.length) return;
  const ok = await app.ask(
    `Update ${card.title}?`,
    `Downloads the newest image, recreates ${services.join(', ')} and removes old unused images. ${card.title} will be unavailable for a few seconds.`,
    'Update',
  );
  if (ok) composeAction(card.project, 'update', services);
}

export function grafanaUrl(c: Container): string | null {
  const g = app.containers.find((x) => x.appKey === 'grafana' && x.webPort);
  const base = g ? `${g.webScheme || 'http'}://${app.layout.settings.linkHost || location.hostname}:${g.webPort}` : '';
  const tpl = app.layout.settings.grafana;
  if (tpl) {
    return tpl
      .replaceAll('{grafana}', base)
      .replaceAll('{name}', encodeURIComponent(c.name))
      .replaceAll('{project}', encodeURIComponent(c.project || ''))
      .replaceAll('{service}', encodeURIComponent(c.service || ''));
  }
  if (!base) return null;
  const expr = `sum(rate(container_cpu_usage_seconds_total{name="${c.name}"}[1m]))`;
  const panes = { a: { queries: [{ refId: 'A', expr }], range: { from: 'now-6h', to: 'now' } } };
  return `${base}/explore?schemaVersion=1&orgId=1&panes=${encodeURIComponent(JSON.stringify(panes))}`;
}
