// Global app state (Svelte 5 runes) + the live event stream.

import { api, ApiError } from './api';
import { clone, normalizeLayout } from './layout';
import type { Container, HostInfo, Job, Layout, Sample } from './types';
import type { DragState } from './dnd';

const HISTORY = 100;
const clientId = Math.random().toString(36).slice(2);

interface Init {
  auth: boolean;
  setup?: boolean;
  passwordManaged?: boolean;
  authEnabled?: boolean;
  version?: string;
  containers?: Container[];
  history?: Record<string, Sample[]>;
  host?: HostInfo;
  layout?: unknown;
  rev?: number;
  jobs?: Job[];
  updatesChecked?: number;
}

class AppState {
  auth = $state(false);
  setup = $state(false); // first run: no password chosen yet
  passwordManaged = $state(false); // password comes from THEBAR_PASSWORD
  authEnabled = $state(true);
  version = $state('');
  containers = $state<Container[]>([]);
  history = $state<Record<string, Sample[]>>({});
  host = $state<HostInfo | null>(null);
  layout = $state<Layout>(normalizeLayout(null));
  rev = 0;
  jobs = $state<Job[]>([]);
  updatesChecked = $state(0);
  connected = $state(true);

  // ui
  drag = $state<DragState | null>(null); // card or group being dragged
  renaming = $state<string | null>(null); // group id whose name is being edited
  query = $state('');
  expanded = $state<string | null>(null);
  // where the expanded card is shown: before the first item of the row it was
  // in, so it opens in place instead of dropping below its row
  expandAnchor = $state<{ key: string; before: string } | null>(null);
  now = $state(Date.now());
  jobsOpen = $state(false);
  modal = $state<null | { kind: 'ports' } | { kind: 'settings' } | { kind: 'compose'; project: string; service?: string }>(null);
  toasts = $state<{ id: number; text: string; kind: 'info' | 'error' | 'ok' }[]>([]);
  confirm = $state<null | { title: string; body: string; ok: string; danger?: boolean; resolve: (v: boolean) => void }>(null);

  private es: EventSource | null = null;
  private saveTimer: ReturnType<typeof setTimeout> | null = null;

  load(init: Init) {
    this.auth = init.auth;
    this.setup = !!init.setup;
    if (!init.auth) return;
    this.passwordManaged = !!init.passwordManaged;
    this.authEnabled = init.authEnabled ?? true;
    this.version = init.version || '';
    this.containers = init.containers || [];
    this.history = init.history || {};
    this.host = init.host || null;
    this.layout = normalizeLayout(init.layout);
    this.rev = init.rev || 0;
    this.jobs = init.jobs || [];
    this.updatesChecked = init.updatesChecked || 0;
    setInterval(() => (this.now = Date.now()), 15000);
    this.connect();
  }

  connect() {
    this.es?.close();
    const es = new EventSource('/api/events');
    this.es = es;
    es.onopen = () => (this.connected = true);
    es.onerror = () => {
      this.connected = false;
      // a 401 also lands here; check if we're still logged in
      fetch('/api/init', { credentials: 'same-origin' }).then((r) => {
        if (r.status === 401) location.reload();
      });
    };
    es.addEventListener('containers', (e) => {
      this.containers = JSON.parse((e as MessageEvent).data);
    });
    es.addEventListener('host', (e) => {
      this.host = JSON.parse((e as MessageEvent).data);
    });
    es.addEventListener('stats', (e) => {
      const latest: Record<string, Sample> = JSON.parse((e as MessageEvent).data);
      const h = this.history;
      for (const [id, s] of Object.entries(latest)) {
        const arr = h[id] ? [...h[id], s] : [s];
        h[id] = arr.length > HISTORY ? arr.slice(arr.length - HISTORY) : arr;
      }
      // drop history of containers that stopped
      for (const id of Object.keys(h)) if (!(id in latest)) delete h[id];
    });
    es.addEventListener('layout', (e) => {
      const d = JSON.parse((e as MessageEvent).data);
      if (d.origin === clientId) {
        this.rev = Math.max(this.rev, d.rev);
        return;
      }
      this.layout = normalizeLayout(d.layout);
      this.rev = d.rev;
    });
    es.addEventListener('job', (e) => {
      const { job, line } = JSON.parse((e as MessageEvent).data) as { job: Job; line: string };
      const i = this.jobs.findIndex((j) => j.id === job.id);
      if (i === -1) {
        this.jobs.push({ ...job, lines: line ? [line] : [] });
        if (this.jobs.length > 30) this.jobs.splice(0, this.jobs.length - 30);
      } else {
        const j = this.jobs[i];
        j.state = job.state;
        j.ended = job.ended;
        if (line) j.lines.push(line);
        if (job.state === 'ok') this.toast(`Done: ${job.title}`, 'ok');
        if (job.state === 'failed') this.toast(`Failed: ${job.title}`, 'error');
      }
    });
  }

  /** Change the layout and save it (debounced). */
  updateLayout(fn: (l: Layout) => void) {
    const next = clone(this.layout);
    fn(next);
    this.layout = next;
    if (this.saveTimer) clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => this.saveLayout(), 300);
  }

  private saving = false;
  private savePending = false;

  // One save at a time: overlapping saves would carry a stale revision, get
  // rejected as a conflict and undo the newer change.
  private async saveLayout() {
    if (this.saving) {
      this.savePending = true;
      return;
    }
    this.saving = true;
    try {
      const res = await api.put<{ rev: number }>('/api/layout', { layout: this.layout, rev: this.rev, origin: clientId });
      this.rev = res.rev;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        // someone else saved in between: take theirs
        this.layout = normalizeLayout(e.body?.layout);
        this.rev = e.body?.rev ?? this.rev;
        this.toast('The layout was changed in another window, showing the latest version.', 'info');
      } else {
        this.toast('Could not save layout: ' + (e as Error).message, 'error');
      }
    } finally {
      this.saving = false;
      if (this.savePending) {
        this.savePending = false;
        this.saveLayout();
      }
    }
  }

  toast(text: string, kind: 'info' | 'error' | 'ok' = 'info') {
    const id = Math.random();
    this.toasts.push({ id, text, kind });
    setTimeout(() => (this.toasts = this.toasts.filter((t) => t.id !== id)), kind === 'error' ? 8000 : 4000);
  }

  ask(title: string, body: string, ok = 'OK', danger = false): Promise<boolean> {
    return new Promise((resolve) => {
      this.confirm = { title, body, ok, danger, resolve };
    });
  }
}

export const app = new AppState();

// ---- actions ----

export async function containerAction(id: string, action: 'start' | 'stop' | 'restart') {
  await api.post(`/api/containers/${id}/${action}`);
}

export async function composeAction(project: string, action: string, services: string[] = []) {
  try {
    await api.post(`/api/compose/${encodeURIComponent(project)}/action`, { action, services });
    app.jobsOpen = true;
  } catch (e) {
    app.toast((e as Error).message, 'error');
  }
}
