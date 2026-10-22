import type { Sample } from './types';

export interface Agg {
  cpu: number[];
  mem: number[];
  rx: number[];
  tx: number[];
  br: number[];
  bw: number[];
  t: number[];
  hasNet: boolean;
  limit: number;
}

/** Sum the histories of several containers, aligned from the newest sample. */
export function aggregate(history: Record<string, Sample[]>, ids: string[]): Agg {
  const series = ids.map((id) => history[id]).filter((s): s is Sample[] => !!s && s.length > 0);
  const n = series.reduce((m, s) => Math.max(m, s.length), 0);
  const out: Agg = { cpu: [], mem: [], rx: [], tx: [], br: [], bw: [], t: [], hasNet: false, limit: 0 };
  for (let i = 0; i < n; i++) {
    let cpu = 0, mem = 0, rx = 0, tx = 0, br = 0, bw = 0, t = 0;
    for (const s of series) {
      const j = s.length - n + i;
      if (j < 0) continue;
      const x = s[j];
      cpu += x.cpu;
      mem += x.mem;
      rx += x.rx;
      tx += x.tx;
      br += x.br;
      bw += x.bw;
      t = Math.max(t, x.t);
    }
    out.cpu.push(cpu);
    out.mem.push(mem);
    out.rx.push(rx);
    out.tx.push(tx);
    out.br.push(br);
    out.bw.push(bw);
    out.t.push(t);
  }
  for (const s of series) {
    const last = s[s.length - 1];
    if (last.net) out.hasNet = true;
    out.limit = Math.max(out.limit, last.limit);
  }
  return out;
}

export function last(a: number[]): number {
  return a.length ? a[a.length - 1] : 0;
}
