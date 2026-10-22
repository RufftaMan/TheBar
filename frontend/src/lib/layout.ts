// Turns the raw container list + the saved layout into groups and cards.
// Pure functions only: easy to reason about, cheap to recompute.

import type { CardPrefs, CardStatus, CardView, Container, GroupView, Layout, LinkInfo, TopItem } from './types';
import { pretty } from './format';

export function emptyLayout(): Layout {
  return {
    v: 1,
    top: [],
    groups: {},
    cards: {},
    dissolved: [],
    seen: [],
    settings: { preferDomain: true, linkHost: '', grafana: '' },
  };
}

/** Fill in fields missing from an older/partial saved layout. */
export function normalizeLayout(raw: any): Layout {
  const l = emptyLayout();
  if (!raw || typeof raw !== 'object') return l;
  return {
    v: 1,
    top: Array.isArray(raw.top) ? raw.top : [],
    groups: raw.groups && typeof raw.groups === 'object' ? raw.groups : {},
    cards: raw.cards && typeof raw.cards === 'object' ? raw.cards : {},
    dissolved: Array.isArray(raw.dissolved) ? raw.dissolved : [],
    seen: Array.isArray(raw.seen) ? raw.seen : [],
    settings: { ...l.settings, ...(raw.settings || {}) },
  };
}

const SERIES_RE = /^(.+?)[-_](\d{4}-\d{2}-\d{2}|\d{8}|\d+)$/;

interface RawCard {
  key: string;
  containers: Container[];
  project?: string;
  service?: string;
  series: number;
  seriesPrefix?: string;
  primary: boolean;
}

function firstSeg(s = '') {
  return s.split(/[-_]/)[0];
}

/** Group containers into raw cards (one per compose service / series / container). */
function rawCards(containers: Container[], prefs: Record<string, CardPrefs>): Map<string, RawCard> {
  // detect series among non-compose containers: name-<date|number>, 3 or more
  const prefixCount = new Map<string, number>();
  for (const c of containers) {
    if (c.project) continue;
    const m = c.name.match(SERIES_RE);
    if (m) prefixCount.set(m[1], (prefixCount.get(m[1]) || 0) + 1);
  }
  const cards = new Map<string, RawCard>();
  for (const c of containers) {
    let key: string;
    let series = 0;
    let seriesPrefix: string | undefined;
    if (c.project && c.service) {
      key = `${c.project}/${c.service}`;
    } else {
      const m = c.name.match(SERIES_RE);
      if (m && (prefixCount.get(m[1]) || 0) >= 3) {
        key = `series:${m[1]}`;
        seriesPrefix = m[1];
        series = prefixCount.get(m[1])!;
      } else {
        key = c.name;
      }
    }
    let rc = cards.get(key);
    if (!rc) {
      rc = { key, containers: [], project: c.project, service: c.service, series, seriesPrefix, primary: false };
      cards.set(key, rc);
    }
    rc.containers.push(c);
  }
  for (const rc of cards.values()) {
    rc.containers.sort((a, b) => a.name.localeCompare(b.name));
    const p = prefs[rc.key];
    rc.primary =
      !!p?.url || (rc.series === 0 && rc.containers.some((c) => (c.webPort || 0) > 0 && !c.app?.helper));
  }
  return cards;
}

/** Decide which card a helper (db, cache, exporter...) folds into. */
function autoParent(rc: RawCard, all: Map<string, RawCard>): string | undefined {
  if (rc.primary || rc.series || !rc.project) return undefined;
  const prims = [...all.values()].filter((o) => o.primary && o.project === rc.project && o.key !== rc.key);
  if (prims.length === 0) return undefined;
  const svc = rc.service || '';
  // 1. name prefix: "fusionauth-db" -> "fusionauth", "osrm-eras" -> "osrm"
  const byPrefix = prims
    .filter((p) => p.service && (svc.startsWith(p.service + '-') || svc.startsWith(p.service + '_')))
    .sort((a, b) => (b.service!.length || 0) - (a.service!.length || 0));
  if (byPrefix.length) return byPrefix[0].key;
  // 2. shared first word: "immich-machine-learning" -> "immich-server"
  const seg = prims.filter((p) => firstSeg(p.service) === firstSeg(svc) && firstSeg(svc).length > 2);
  if (seg.length === 1) return seg[0].key;
  // 3. whoever depends on it
  const deps = prims.filter((p) => p.containers.some((c) => c.dependsOn?.includes(svc)));
  if (deps.length) return deps.sort((a, b) => a.key.localeCompare(b.key))[0].key;
  // 4. a database/cache with no clear owner belongs to the stack's backend
  if (rc.containers[0]?.app?.helper) {
    const be = prims.filter((p) => /(^|[-_])(backend|api|server|app)([-_]|$)/.test(p.service || ''));
    if (be.length === 1) return be[0].key;
  }
  // 5. known hint: exporters -> prometheus
  const hint = rc.containers[0]?.app?.helperOf;
  if (hint) {
    const h = prims.find((p) => p.containers.some((c) => c.appKey === hint));
    if (h) return h.key;
  }
  // 6. the only app in the stack
  if (prims.length === 1) return prims[0].key;
  return undefined;
}

function statusOf(containers: Container[]): CardStatus {
  const total = containers.length;
  const running = containers.filter((c) => c.state === 'running').length;
  const alert = containers.find((c) => c.alert);
  if (alert) return { level: 'critical', label: alert.alert!, running, total };
  if (containers.some((c) => c.state === 'restarting')) return { level: 'warning', label: 'Restarting', running, total };
  if (running === 0) {
    if (containers.some((c) => c.state === 'paused')) return { level: 'warning', label: 'Paused', running, total };
    return { level: 'stopped', label: 'Stopped', running, total };
  }
  if (running < total) return { level: 'warning', label: `${running}/${total} running`, running, total };
  if (containers.some((c) => c.health === 'starting')) return { level: 'warning', label: 'Starting', running, total };
  if (containers.some((c) => c.health === 'healthy')) return { level: 'good', label: 'Healthy', running, total };
  return { level: 'good', label: 'Running', running, total };
}

export function aggregateStatus(cards: CardView[]): CardStatus {
  const total = cards.length;
  const up = cards.filter((c) => c.status.level === 'good').length;
  const crit = cards.filter((c) => c.status.level === 'critical').length;
  const warn = cards.filter((c) => c.status.level === 'warning').length;
  const stopped = cards.filter((c) => c.status.level === 'stopped').length;
  if (crit) return { level: 'critical', label: `${crit} down`, running: up, total };
  if (warn) return { level: 'warning', label: `${warn} need${warn === 1 ? 's' : ''} attention`, running: up, total };
  if (stopped === total) return { level: 'stopped', label: 'All stopped', running: 0, total };
  if (stopped) return { level: 'good', label: `${up}/${total} up`, running: up, total };
  return { level: 'good', label: `${up}/${total} up`, running: up, total };
}

export function iconUrl(icon: string | undefined): string {
  if (!icon) return '';
  if (/^https?:\/\//.test(icon) || icon.startsWith('/')) return icon;
  return `/icons/di/${icon}.svg`; // picker stores the slug; the server knows the real extension
}

/** Only http(s) addresses are links; anything else (javascript:, data:, …) is ignored. */
function webUrl(u: string | undefined): string | undefined {
  if (!u) return undefined;
  try {
    const p = new URL(u).protocol;
    return p === 'http:' || p === 'https:' ? u : undefined;
  } catch {
    return undefined;
  }
}

function linksFor(main: Container, own: Container[], prefs: CardPrefs | undefined, layout: Layout, host: string) {
  const links: LinkInfo[] = [];
  const custom = webUrl(prefs?.url);
  if (custom) links.push({ label: 'Custom link', url: custom });
  for (const c of own) {
    for (const d of c.domains || []) links.push({ label: 'Proxy', url: d });
  }
  for (const c of own) {
    if (c.networkMode === 'host' && c.webPort) {
      links.push({ label: `Port ${c.webPort}`, url: `${c.webScheme || 'http'}://${host}:${c.webPort}` });
    }
    for (const p of c.ports) {
      if (p.proto !== 'tcp') continue;
      const https = p.host === c.webPort ? c.webScheme === 'https' : [443, 8443, 9443].includes(p.container);
      links.push({ label: `Port ${p.host}`, url: `${https ? 'https' : 'http'}://${host}:${p.host}` });
    }
  }
  // dedupe
  const seen = new Set<string>();
  const uniq = links.filter((l) => (seen.has(l.url) ? false : (seen.add(l.url), true)));
  let url: string | null = null;
  if (custom) url = custom;
  else if (layout.settings.preferDomain && main.domains?.length) url = main.domains[0];
  else if (main.webPort) url = `${main.webScheme || 'http'}://${host}:${main.webPort}`;
  else url = own.find((c) => c.webPort) ? uniq.find((l) => l.label.startsWith('Port'))?.url || null : null;
  return { url, links: uniq };
}

// Default order uses names the user can't edit. Sorting by nickname would move
// a card while you type its new name, and moving a focused input blurs it.
function byStableName(a: CardView, b: CardView) {
  return a.appName.localeCompare(b.appName) || a.key.localeCompare(b.key);
}

export interface View {
  top: TopItem[];
  hidden: CardView[];
  cards: Map<string, CardView>; // every card (visible + hidden), by key
  groups: Map<string, GroupView>;
  autoGroupOf: Map<string, string>; // card key -> its default group id
}

export function buildView(containers: Container[], layout: Layout, linkHost: string): View {
  const prefs = layout.cards;
  const raws = rawCards(containers, prefs);

  // resolve parents (explicit beats automatic), flatten chains, avoid cycles
  const parentOf = new Map<string, string>();
  for (const rc of raws.values()) {
    const p = prefs[rc.key]?.parent;
    let target: string | undefined;
    if (p === '') target = undefined;
    else if (p && raws.has(p) && p !== rc.key) target = p;
    else target = autoParent(rc, raws);
    if (target) parentOf.set(rc.key, target);
  }
  const rootOf = (key: string) => {
    let k = key;
    for (let i = 0; i < 10 && parentOf.has(k); i++) k = parentOf.get(k)!;
    return parentOf.has(k) ? key : k; // cycle: stay put
  };
  const children = new Map<string, RawCard[]>();
  for (const rc of raws.values()) {
    const root = rootOf(rc.key);
    if (root !== rc.key) {
      if (!children.has(root)) children.set(root, []);
      children.get(root)!.push(rc);
    }
  }

  const seen = new Set(layout.seen);
  const cards = new Map<string, CardView>();
  for (const rc of raws.values()) {
    if (rootOf(rc.key) !== rc.key) continue;
    const p = prefs[rc.key];
    const own = rc.containers;
    const main = own.find((c) => c.webPort) || own.find((c) => c.state === 'running') || own[0];
    const kids = children.get(rc.key) || [];
    const folded = kids.map((k) => ({
      key: k.key,
      title: k.containers[0]?.app?.name && !k.containers[0]?.app?.helper ? k.containers[0].app!.name : pretty(k.service || k.containers[0].name),
      containers: k.containers,
    }));
    const all = [...own, ...kids.flatMap((k) => k.containers)];
    const appName = main.self
      ? 'TheBar'
      : rc.series
        ? pretty(rc.seriesPrefix!)
        : main.app && !main.app.helper
          ? main.app.name
          : pretty(rc.service || main.name);
    const suggested = main.self ? 'This dashboard' : rc.series ? undefined : main.app?.nickname;
    const nickname = p?.nickname !== undefined ? p.nickname : suggested && !main.app?.helper ? suggested : '';
    const title = nickname || appName;
    const subtitle = nickname ? appName : rc.series ? `${rc.series} containers` : main.name;
    const { url, links } = linksFor(main, own, p, layout, linkHost);
    const icon = p?.icon ? iconUrl(p.icon) : main.self ? '/favicon.svg' : main.icon || '';
    const card: CardView = {
      key: rc.key,
      title,
      subtitle,
      appName,
      own,
      folded,
      all,
      main,
      project: rc.project || undefined,
      service: rc.service || undefined,
      url,
      links,
      icon,
      status: statusOf(rc.series ? own : all),
      isNew: layout.seen.length > 0 && !seen.has(rc.key),
      hidden: !!p?.hidden,
      series: rc.series,
      update: all.some((c) => c.update === 'available'),
      recreate: all.some((c) => c.update === 'recreate'),
      drift: all.some((c) => c.drift),
      note: p?.note || '',
      search: '',
    };
    card.search = [
      title,
      nickname,
      appName,
      p?.note,
      rc.project,
      ...all.map((c) => `${c.name} ${c.image}`),
      ...(own.flatMap((c) => c.domains || [])),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
    cards.set(rc.key, card);
  }

  // ---- groups ----
  const visible = [...cards.values()].filter((c) => !c.hidden);
  const byProject = new Map<string, CardView[]>();
  const projectOf = (c: CardView): string | undefined => {
    if (c.project) return c.project;
    if (c.series) {
      // "myapp-nightly" belongs with the "myapp" project
      const projects = new Set(containers.map((x) => x.project).filter(Boolean) as string[]);
      const prefix = c.key.slice('series:'.length);
      return [...projects].sort((a, b) => b.length - a.length).find((p) => prefix === p || prefix.startsWith(p + '-') || prefix.startsWith(p + '_'));
    }
    return undefined;
  };
  for (const c of visible) {
    const p = projectOf(c);
    if (!p) continue;
    if (!byProject.has(p)) byProject.set(p, []);
    byProject.get(p)!.push(c);
  }
  const dissolved = new Set(layout.dissolved);
  const groupIds = new Set<string>(Object.keys(layout.groups).filter((id) => !id.startsWith('p:') || !dissolved.has(id.slice(2))));
  const autoGroupOf = new Map<string, string>();
  for (const [p, list] of byProject) {
    const id = `p:${p}`;
    if (dissolved.has(p)) continue;
    if (list.length >= 2 || groupIds.has(id)) {
      groupIds.add(id);
      for (const c of list) autoGroupOf.set(c.key, id);
    }
  }

  // explicit membership first
  const memberOf = new Map<string, string>();
  for (const id of groupIds) {
    for (const k of layout.groups[id]?.members || []) {
      if (cards.has(k) && !memberOf.has(k)) memberOf.set(k, id);
    }
  }
  const topSet = new Set(layout.top);
  const groupCards = new Map<string, CardView[]>();
  for (const id of groupIds) groupCards.set(id, []);
  const topCards: CardView[] = [];
  const sortedVisible = [...visible].sort(byStableName);
  for (const c of sortedVisible) {
    let g = memberOf.get(c.key);
    if (!g && !topSet.has('c:' + c.key)) g = autoGroupOf.get(c.key);
    if (g && groupCards.has(g)) {
      c.groupId = g;
      groupCards.get(g)!.push(c);
    } else {
      topCards.push(c);
    }
  }

  const groups = new Map<string, GroupView>();
  for (const id of groupIds) {
    const gp = layout.groups[id] || {};
    const list = groupCards.get(id)!;
    const order = gp.members || [];
    list.sort((a, b) => {
      const ia = order.indexOf(a.key);
      const ib = order.indexOf(b.key);
      if (ia === -1 && ib === -1) return 0; // keep alphabetical
      if (ia === -1) return 1;
      if (ib === -1) return -1;
      return ia - ib;
    });
    const project = id.startsWith('p:') ? id.slice(2) : undefined;
    groups.set(id, {
      id,
      name: gp.name || (project ? pretty(project) : 'Group'),
      project,
      cards: list,
      collapsed: !!gp.collapsed,
      status: aggregateStatus(list),
    });
  }

  // top-level order: saved order first, then anything new
  const items = new Map<string, TopItem>();
  for (const c of topCards) items.set('c:' + c.key, { kind: 'card', id: 'c:' + c.key, card: c });
  for (const g of groups.values()) items.set('g:' + g.id, { kind: 'group', id: 'g:' + g.id, group: g });
  const top: TopItem[] = [];
  for (const id of layout.top) {
    const it = items.get(id);
    if (it) {
      top.push(it);
      items.delete(id);
    }
  }
  // new items: cards before groups, alphabetical
  const rest = [...items.values()].sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'card' ? -1 : 1;
    if (a.kind === 'card' && b.kind === 'card') return byStableName(a.card, b.card);
    return a.id.localeCompare(b.id);
  });
  top.push(...rest);

  const hidden = [...cards.values()].filter((c) => c.hidden).sort(byStableName);
  return { top, hidden, cards, groups, autoGroupOf };
}

/** Rank cards for the search box. */
export function searchCards(view: View, q: string): CardView[] {
  const terms = q.toLowerCase().trim().split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  const scored: { c: CardView; s: number }[] = [];
  for (const c of view.cards.values()) {
    if (!terms.every((t) => c.search.includes(t))) continue;
    const t = c.title.toLowerCase();
    let s = 0;
    if (t.startsWith(terms[0])) s += 10;
    if (t.includes(terms[0])) s += 5;
    if (c.appName.toLowerCase().startsWith(terms[0])) s += 4;
    if (c.url) s += 2;
    if (c.hidden) s -= 3;
    scored.push({ c, s });
  }
  scored.sort((a, b) => b.s - a.s || a.c.title.localeCompare(b.c.title));
  return scored.map((x) => x.c);
}

/** Snapshot of where everything currently is, used when arranging. */
export function snapshotLayout(view: View, layout: Layout): Layout {
  const next: Layout = clone(layout);
  next.top = view.top.map((i) => i.id);
  for (const g of view.groups.values()) {
    next.groups[g.id] = { ...(next.groups[g.id] || {}), members: g.cards.map((c) => c.key) };
  }
  return next;
}

// $state proxies can't be structuredClone'd; a JSON round-trip is fine for this data
export function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v));
}
