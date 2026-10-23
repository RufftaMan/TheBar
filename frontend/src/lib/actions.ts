import { tick } from 'svelte';
import { app, composeAction, containerAction } from './store.svelte';
import type { CardView, Container, GroupView } from './types';

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

// ---- group actions: work for every group, automatic or made by hand ----

export interface GroupPlan {
  containers: Container[]; // everything in the group's cards (helpers included), without TheBar itself
  byProject: Map<string, Container[]>; // compose containers per project
  loose: Container[]; // containers that don't belong to a compose project
  running: number;
  stopped: number;
  update: Map<string, string[]>; // project -> services that have a newer image
  drift: Map<string, string[]>; // project -> services whose compose file changed
  updateCount: number;
  driftCount: number;
  hasSelf: boolean;
}

function services(list: Container[]): string[] {
  return [...new Set(list.map((c) => c.service!).filter(Boolean))];
}

export function planGroup(group: GroupView): GroupPlan {
  const all = group.cards.flatMap((c) => c.all);
  const containers = all.filter((c) => !c.self); // TheBar can't stop or update itself from here
  const byProject = new Map<string, Container[]>();
  const loose: Container[] = [];
  for (const c of containers) {
    if (c.project && c.service) {
      if (!byProject.has(c.project)) byProject.set(c.project, []);
      byProject.get(c.project)!.push(c);
    } else {
      loose.push(c);
    }
  }
  const pick = (pred: (c: Container) => boolean) => {
    const m = new Map<string, string[]>();
    for (const [project, list] of byProject) {
      const sv = services(list.filter(pred));
      if (sv.length) m.set(project, sv);
    }
    return m;
  };
  const update = pick((c) => c.update === 'available' || c.update === 'recreate');
  const drift = pick((c) => !!c.drift);
  const count = (m: Map<string, string[]>) => [...m.values()].reduce((n, v) => n + v.length, 0);
  return {
    containers,
    byProject,
    loose,
    running: containers.filter((c) => c.state === 'running' || c.state === 'restarting').length,
    stopped: containers.filter((c) => c.state !== 'running' && c.state !== 'restarting').length,
    update,
    drift,
    updateCount: count(update),
    driftCount: count(drift),
    hasSelf: all.some((c) => c.self),
  };
}

function names(list: string[], max = 6): string {
  return list.length > max ? `${list.slice(0, max).join(', ')} and ${list.length - max} more` : list.join(', ');
}

/** Start, stop or restart everything in a group, or update / apply config where needed. */
export async function groupAction(group: GroupView, action: 'start' | 'stop' | 'restart' | 'update' | 'apply') {
  const plan = planGroup(group);
  const alive = (c: Container) => c.state === 'running' || c.state === 'restarting';
  const want = (c: Container) => (action === 'start' ? !alive(c) : alive(c));

  // what to do, per compose project (as one job each) and for loose containers (one by one)
  const perProject = new Map<string, string[]>();
  let loose: Container[] = [];
  if (action === 'update' || action === 'apply') {
    for (const [p, sv] of action === 'update' ? plan.update : plan.drift) perProject.set(p, sv);
  } else {
    for (const [p, list] of plan.byProject) {
      const sv = services(list.filter(want));
      if (sv.length) perProject.set(p, sv);
    }
    loose = plan.loose.filter(want);
  }
  const serviceCount = [...perProject.values()].reduce((n, v) => n + v.length, 0);
  const total = serviceCount + loose.length;
  if (!total) {
    app.toast(
      action === 'start' ? 'Everything in this group is already running.' : action === 'update' ? 'Nothing to update.' : action === 'apply' ? 'No config changes to apply.' : 'Nothing is running in this group.',
    );
    return;
  }

  const labels = { start: 'Start', stop: 'Stop', restart: 'Restart', update: 'Update', apply: 'Apply config' }[action];
  const involved = [...[...perProject.values()].flat(), ...loose.map((c) => c.name)];
  const body =
    {
      start: `Starts ${names(involved)}.`,
      stop: `Stops ${total} container${total === 1 ? '' : 's'}: ${names(involved)}.`,
      restart: `Restarts ${total} container${total === 1 ? '' : 's'}: ${names(involved)}.`,
      update: `Downloads newer images and recreates ${names(involved)}. They will be unavailable for a few seconds.`,
      apply: `Recreates ${names(involved)} with the changed compose file.`,
    }[action] + (plan.hasSelf ? ' TheBar itself is skipped.' : '');
  if (action !== 'start' && !(await app.ask(`${labels} ${group.name}?`, body, labels, action === 'stop'))) return;

  for (const [project, sv] of perProject) composeAction(project, action === 'start' ? 'up' : action, sv);
  for (const c of loose) {
    try {
      await containerAction(c.id, action as 'start' | 'stop' | 'restart');
    } catch (e) {
      app.toast(`${c.name}: ${(e as Error).message}`, 'error');
      return;
    }
  }
  if (loose.length && !perProject.size) app.toast(`${group.name}: ${action} done`, 'ok');
}
