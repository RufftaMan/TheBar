const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function bytes(n: number | undefined, digits = 1): string {
  if (!n || n < 0) return '0 B';
  let i = 0;
  while (n >= 1000 && i < UNITS.length - 1) {
    n /= 1000;
    i++;
  }
  const d = n >= 100 || i === 0 ? 0 : digits;
  return `${n.toFixed(d)} ${UNITS[i]}`;
}

export function rate(n: number | undefined): string {
  if (!n || n < 1) return '0 B/s';
  return bytes(n) + '/s';
}

export function pct(n: number | undefined, digits = 1): string {
  if (n === undefined || isNaN(n)) return '–';
  if (n > 0 && n < 0.01) return '<0.01%';
  if (n > 0 && n < 1 && digits > 0) return n.toFixed(2) + '%'; // small container loads stay readable
  return n.toFixed(n >= 100 ? 0 : digits) + '%';
}

/** "2.8 of 12 cores" for a whole-machine CPU percentage. */
export function cores(p: number, total: number | undefined): string {
  if (!total) return '';
  const c = (p / 100) * total;
  if (c > 0 && c < 0.005) return `<0.01 of ${total} cores`;
  return `${c < 0.1 ? c.toFixed(2) : c.toFixed(1)} of ${total} cores`;
}

export function duration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return h ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

export function since(iso: string | undefined, now: number): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (isNaN(t) || t < 946684800000) return '';
  return duration(now - t);
}

export function timeAgo(t: number, now: number): string {
  if (!t) return 'never';
  const d = now - t;
  if (d < 60000) return 'just now';
  return duration(d) + ' ago';
}

/** "frontend-dev" -> "Frontend Dev" */
export function pretty(s: string): string {
  return s
    .replace(/[-_.]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

export function clockTime(t: number): string {
  const d = new Date(t);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}
