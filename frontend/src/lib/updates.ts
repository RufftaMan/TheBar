// What a container's update status means, and how far it can be trusted.
import { timeAgo } from './format';
import type { Container } from './types';

/** Full checks run every 12 hours; a result older than this missed one. */
export const STALE_AFTER = 13 * 3600 * 1000;

export function hasUpdate(c: Container): boolean {
  return c.update === 'available' || c.update === 'recreate';
}

/** The latest lookup failed, or the last answer is older than it should be. */
export function isStale(c: Container, now: number): boolean {
  if (c.update === 'local') return false;
  return !!c.updateFailed || (!!c.updateChecked && now - c.updateChecked > STALE_AFTER);
}

export function staleText(c: Container, now: number): string {
  if (!c.updateFailed) return `Last checked ${timeAgo(c.updateChecked!, now)}`;
  if (!c.updateChecked) return 'Update check failed';
  return `Update check failed, last answer ${timeAgo(c.updateChecked, now)}`;
}

/** One line about a whole card: up to date, unknown, ... and how old that is. */
export function updateSummary(list: Container[], now: number): { text: string; checked: number } {
  const remote = list.filter((c) => c.update !== 'local');
  const times = remote.map((c) => c.updateChecked || 0).filter(Boolean);
  const checked = times.length ? Math.min(...times) : 0; // the oldest answer counts
  const outdated = list.filter(hasUpdate);
  let text: string;
  if (outdated.length) text = `Update for ${outdated.map((c) => c.name).join(', ')}`;
  else if (!remote.length) text = 'Not checked: local or pinned image';
  else if (remote.some((c) => c.updateFailed)) text = 'Unknown: the latest check failed';
  else if (remote.some((c) => !c.update)) text = 'Not checked yet';
  else if (remote.some((c) => isStale(c, now))) text = 'No update at the last check';
  else text = 'Up to date';
  return { text, checked };
}
