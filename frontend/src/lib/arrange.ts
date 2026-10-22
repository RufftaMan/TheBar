import { app } from './store.svelte';
import { snapshotLayout, type View } from './layout';
import { pretty } from './format';
import type { DragState, DropTarget } from './dnd';
import type { Layout } from './types';

/** Apply a drag & drop move to the layout. */
export function applyMove(view: View, d: DragState, t: DropTarget) {
  const next = snapshotLayout(view, app.layout);
  // work on prefixed ids everywhere ("c:<key>"); group members go back to bare keys at the end
  const lists = new Map<string, string[]>();
  lists.set('top', next.top);
  for (const [gid, g] of Object.entries(next.groups)) lists.set('g:' + gid, (g.members || []).map((k) => 'c:' + k));
  const list = (id: string) => {
    if (!lists.has(id)) lists.set(id, []);
    return lists.get(id)!;
  };

  // take it out of where it was
  for (const l of lists.values()) {
    const i = l.indexOf(d.id);
    if (i >= 0) l.splice(i, 1);
  }

  if (t.type === 'into') {
    list('g:' + t.group).push(d.id);
  } else if (t.type === 'merge') {
    // dropped onto a top-level card: both become a new group in its place
    const gid = 'u:' + Math.random().toString(36).slice(2, 8);
    const top = list('top');
    const at = top.indexOf(t.id);
    top.splice(at < 0 ? top.length : at, at < 0 ? 0 : 1, 'g:' + gid);
    lists.set('g:' + gid, [t.id, d.id]);
    next.groups[gid] = { name: groupName(view, t.id, d.id) };
    app.renaming = gid;
  } else {
    const l = list(t.list);
    const i = l.indexOf(t.id);
    l.splice(i < 0 ? l.length : t.type === 'after' ? i + 1 : i, 0, d.id);
  }

  for (const [id, l] of lists) {
    if (!id.startsWith('g:')) continue;
    const gid = id.slice(2);
    next.groups[gid] = { ...(next.groups[gid] || {}), members: l.map((k) => k.slice(2)) };
  }
  pruneEmptyGroups(next);
  app.updateLayout((l) => Object.assign(l, next));
}

/** Groups without cards disappear (an automatic one also stays gone). */
function pruneEmptyGroups(l: Layout) {
  for (const [gid, g] of Object.entries(l.groups)) {
    if (g.members && g.members.length === 0) {
      delete l.groups[gid];
      l.top = l.top.filter((x) => x !== 'g:' + gid);
      if (gid.startsWith('p:') && !l.dissolved.includes(gid.slice(2))) l.dissolved.push(gid.slice(2));
    }
  }
}

/** A new group is named after the stack both cards come from, if they share one. */
function groupName(view: View, a: string, b: string): string {
  const pa = view.cards.get(a.slice(2))?.project;
  const pb = view.cards.get(b.slice(2))?.project;
  return pa && pa === pb ? pretty(pa) : 'New group';
}

/** Remove a group; its cards move to the top level where the group was. */
export function dissolveGroup(view: View, gid: string) {
  const g = view.groups.get(gid);
  const next = snapshotLayout(view, app.layout);
  const pos = next.top.indexOf('g:' + gid);
  const members = (g?.cards || []).map((c) => 'c:' + c.key);
  next.top.splice(pos < 0 ? next.top.length : pos, pos < 0 ? 0 : 1, ...members);
  delete next.groups[gid];
  if (gid.startsWith('p:') && !next.dissolved.includes(gid.slice(2))) next.dissolved.push(gid.slice(2));
  app.updateLayout((l) => Object.assign(l, next));
}

export function renameGroup(gid: string, name: string) {
  app.updateLayout((l) => {
    l.groups[gid] = { ...(l.groups[gid] || {}), name };
  });
}

export function toggleCollapse(gid: string, collapsed: boolean) {
  app.updateLayout((l) => {
    l.groups[gid] = { ...(l.groups[gid] || {}), collapsed };
  });
}
