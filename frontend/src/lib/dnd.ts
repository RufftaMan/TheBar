// Home-screen style drag & drop for cards and groups.
//
// Mouse: press and move a few pixels (pointer events).
// Touch: long-press, then drag (touch events: once a drag has started we can
// cancel the touchmoves to stop scrolling; pointer events would be cancelled
// by the browser as soon as it starts panning).
// Nothing in the DOM is moved while dragging: we only track the pointer,
// work out the drop target, show indicators, and on drop hand a "move" to the
// layout code. Svelte then re-renders from the new layout.
//
// Drop targets:
//   card, middle            -> merge: new group with both (top level only)
//   card, left/right half   -> before/after it (same list as that card)
//   group, top/bottom edge  -> before/after the group
//   group, anywhere else    -> into the group (cards only)
//   gap/empty grid space    -> before/after the nearest item

import { app } from './store.svelte';

export type DragKind = 'card' | 'group';

export interface DragOpts {
  id: string; // "c:<key>" or "g:<groupId>"
  list: string; // list it lives in: "top" or "g:<groupId>"
  kind: DragKind;
  title: string;
  icon: string;
  disabled?: boolean;
}

export type DropTarget =
  | { type: 'before' | 'after' | 'merge'; id: string; list: string }
  | { type: 'into'; group: string };

export interface DragState {
  id: string;
  list: string;
  kind: DragKind;
  title: string;
  icon: string;
  x: number;
  y: number;
  target: DropTarget | null;
}

type DropHandler = (drag: DragState, target: DropTarget) => void;
let onDrop: DropHandler = () => {};
export function setDropHandler(fn: DropHandler) {
  onDrop = fn;
}

const NO_DRAG = 'input, textarea, select, button, .no-drag, .cm-editor, .detail';
const EDGE = 14; // px band at a group's top/bottom edge that means "before/after"

// while dragging, stop the page from scrolling under a finger
const blockScrollWhileDragging = (e: TouchEvent) => {
  if (app.drag && e.cancelable) e.preventDefault();
};
if (typeof window !== 'undefined') {
  window.addEventListener('touchmove', blockScrollWhileDragging, { passive: false });
}

function itemOf(el: Element | null): HTMLElement | null {
  return (el?.closest('.item[data-id]') as HTMLElement) || null;
}

function listOf(item: HTMLElement): string {
  return (item.parentElement as HTMLElement | null)?.dataset.list || '';
}

/** Top-level wrapper of an element (cards inside a group map to the group). */
function topItemOf(el: Element | null): HTMLElement | null {
  let item = itemOf(el);
  while (item && listOf(item) !== 'top') item = itemOf(item.parentElement);
  return item;
}

function sideOf(item: HTMLElement, x: number, y: number): 'before' | 'after' {
  const r = item.getBoundingClientRect();
  if (item.classList.contains('wide')) return y < r.top + r.height / 2 ? 'before' : 'after';
  return x < r.left + r.width / 2 ? 'before' : 'after';
}

/** Nearest item of a grid to a point (for gaps and empty space). */
function nearestItem(grid: HTMLElement, x: number, y: number, skip: string): HTMLElement | null {
  let best: HTMLElement | null = null;
  let bestD = Infinity;
  for (const el of grid.children) {
    if (!(el instanceof HTMLElement) || !el.matches('.item[data-id]') || el.dataset.id === skip) continue;
    const r = el.getBoundingClientRect();
    const dx = Math.max(r.left - x, 0, x - r.right);
    const dy = Math.max(r.top - y, 0, y - r.bottom);
    const d = dx * dx + dy * dy;
    if (d < bestD) {
      bestD = d;
      best = el;
    }
  }
  return best;
}

function hitTest(d: DragState, x: number, y: number): DropTarget | null {
  const el = document.elementFromPoint(x, y);
  if (!el) return null;

  if (d.kind === 'group') {
    // groups only move around the top level
    let item = topItemOf(el);
    if (!item) {
      const grid = el.closest('[data-list="top"]') as HTMLElement | null;
      item = grid ? nearestItem(grid, x, y, d.id) : null;
    }
    if (!item || item.dataset.id === d.id) return null;
    return { type: sideOf(item, x, y), id: item.dataset.id!, list: 'top' };
  }

  // a logo tile in a collapsed group
  const mini = el.closest('[data-mini-group]') as HTMLElement | null;
  if (mini) return d.list === 'g:' + mini.dataset.miniGroup ? null : { type: 'into', group: mini.dataset.miniGroup! };

  let item = itemOf(el);
  if (!item) {
    const grid = el.closest('[data-list]') as HTMLElement | null;
    if (!grid) return null;
    item = nearestItem(grid, x, y, d.id);
    if (!item) {
      const list = grid.dataset.list!;
      return list.startsWith('g:') ? { type: 'into', group: list.slice(2) } : null;
    }
    // in a gap: before/after the nearest item, never "onto" it
    return { type: sideOf(item, x, y), id: item.dataset.id!, list: listOf(item) };
  }
  if (item.dataset.id === d.id) return null;
  const id = item.dataset.id!;
  const list = listOf(item);
  const r = item.getBoundingClientRect();

  if (id.startsWith('g:')) {
    // over a group (its header, padding or collapsed bar)
    if (y < r.top + EDGE) return { type: 'before', id, list };
    if (y > r.bottom - EDGE) return { type: 'after', id, list };
    // already in this group: hovering its padding shouldn't move it to the end
    if (d.list === id) return null;
    return { type: 'into', group: id.slice(2) };
  }

  // over a card: the middle merges (top level only), the sides reorder
  const rx = (x - r.left) / r.width;
  const ry = (y - r.top) / r.height;
  if (list === 'top' && rx > 0.25 && rx < 0.75 && ry > 0.15 && ry < 0.85) return { type: 'merge', id, list };
  return { type: rx < 0.5 ? 'before' : 'after', id, list };
}

let scrollRaf = 0;
const EDGE_ZONE = 56; // px band at the screen edge that scrolls the page
const EDGE_DWELL = 280; // ms the finger must stay in the band before the page starts moving
const MAX_SPEED = 560; // px/s right at the edge

function autoScroll() {
  cancelAnimationFrame(scrollRaf);
  let last = performance.now();
  let inZoneSince = 0;
  const step = (now: number) => {
    const d = app.drag;
    if (!d) return;
    const dt = Math.min(now - last, 50) / 1000;
    last = now;
    // the sticky header covers the top of the page: the top band starts below it
    const header = document.querySelector('header.top')?.getBoundingClientRect().bottom ?? 0;
    const topEdge = header + EDGE_ZONE;
    const bottomEdge = innerHeight - EDGE_ZONE;
    let ratio = 0; // 0..1 how deep into a band, negative = up
    if (d.y < topEdge) ratio = -Math.min(1, (topEdge - d.y) / EDGE_ZONE);
    else if (d.y > bottomEdge) ratio = Math.min(1, (d.y - bottomEdge) / EDGE_ZONE);
    if (ratio === 0) {
      inZoneSince = 0;
    } else {
      inZoneSince ||= now;
      if (now - inZoneSince > EDGE_DWELL) {
        const speed = 60 + (MAX_SPEED - 60) * ratio * ratio; // eases in
        const before = scrollY;
        scrollBy(0, Math.sign(ratio) * speed * dt);
        if (scrollY !== before) d.target = hitTest(d, d.x, d.y);
      }
    }
    scrollRaf = requestAnimationFrame(step);
  };
  scrollRaf = requestAnimationFrame(step);
}

// While a touch drag is on, the page must not scroll under the finger. Cancelling
// touchmove works on most browsers, but not all (and never once the browser has
// begun a scroll), so also make the page unscrollable for the duration.
function lockScroll(on: boolean) {
  const root = document.documentElement;
  if (on) {
    root.style.overflow = 'hidden';
    root.style.overscrollBehavior = 'none';
    root.style.touchAction = 'none';
    document.body.style.touchAction = 'none';
  } else {
    root.style.overflow = '';
    root.style.overscrollBehavior = '';
    root.style.touchAction = '';
    document.body.style.touchAction = '';
  }
}

function suppressNextClick() {
  const stop = (e: Event) => {
    e.preventDefault();
    e.stopPropagation();
  };
  window.addEventListener('click', stop, { capture: true, once: true });
  setTimeout(() => window.removeEventListener('click', stop, { capture: true }), 50);
}

/** Svelte action: make an element a drag handle. */
export function draggable(node: HTMLElement, opts: DragOpts) {
  let o = opts;
  let start: { x: number; y: number; id: number; touch: boolean } | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function begin(x: number, y: number) {
    app.drag = { id: o.id, list: o.list, kind: o.kind, title: o.title, icon: o.icon, x, y, target: null };
    document.body.classList.add('dragging');
    if (start?.touch) {
      navigator.vibrate?.(15);
      lockScroll(true);
    }
    autoScroll();
  }

  function finish(drop: boolean) {
    clearTimeout(timer);
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', up);
    window.removeEventListener('pointercancel', cancel);
    window.removeEventListener('touchmove', tmove);
    window.removeEventListener('touchend', tend);
    window.removeEventListener('touchcancel', cancel);
    window.removeEventListener('keydown', key);
    const d = app.drag;
    if (d) {
      cancelAnimationFrame(scrollRaf);
      lockScroll(false);
      document.body.classList.remove('dragging');
      app.drag = null;
      suppressNextClick();
      if (drop && d.target) onDrop(d, d.target);
    }
    start = null;
  }

  function track(x: number, y: number) {
    const d = app.drag;
    if (!d) return;
    d.x = x;
    d.y = y;
    d.target = hitTest(d, x, y);
  }

  // ---- mouse / pen ----
  function move(e: PointerEvent) {
    if (!start || e.pointerId !== start.id) return;
    if (!app.drag) {
      if (Math.hypot(e.clientX - start.x, e.clientY - start.y) > 6) begin(e.clientX, e.clientY);
      return;
    }
    e.preventDefault();
    track(e.clientX, e.clientY);
  }
  const up = (e: PointerEvent) => start && e.pointerId === start.id && finish(true);
  const cancel = () => finish(false);
  const key = (e: KeyboardEvent) => e.key === 'Escape' && finish(false);

  function canStart(target: EventTarget | null): boolean {
    if (o.disabled || app.drag || start) return false;
    const el = target as Element;
    if (el.closest(NO_DRAG)) return false;
    // nested handles (a card inside a group): the innermost one wins
    const handle = el.closest('[data-drag-handle]');
    return !handle || handle === node;
  }

  function down(e: PointerEvent) {
    if (e.pointerType === 'touch' || e.button !== 0 || !canStart(e.target)) return;
    start = { x: e.clientX, y: e.clientY, id: e.pointerId, touch: false };
    window.addEventListener('pointermove', move, { passive: false });
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', cancel);
    window.addEventListener('keydown', key);
  }

  // ---- touch: long-press to pick up ----
  function tstart(e: TouchEvent) {
    if (e.touches.length !== 1 || !canStart(e.target)) return;
    const t = e.touches[0];
    const s = (start = { x: t.clientX, y: t.clientY, id: -1, touch: true });
    timer = setTimeout(() => start === s && begin(s.x, s.y), 350);
    window.addEventListener('touchmove', tmove, { passive: false });
    window.addEventListener('touchend', tend);
    window.addEventListener('touchcancel', cancel);
  }
  function tmove(e: TouchEvent) {
    const t = e.touches[0];
    if (!start || !t) return;
    if (!app.drag) {
      if (Math.hypot(t.clientX - start.x, t.clientY - start.y) > 10) finish(false); // moved early: it's a scroll
      return;
    }
    e.preventDefault(); // we're dragging: don't scroll
    track(t.clientX, t.clientY);
  }
  const tend = () => finish(true);

  // no native drag of links/images, no long-press menu while holding
  const nativeDrag = (e: Event) => e.preventDefault();
  const ctx = (e: Event) => {
    if (start?.touch || app.drag) e.preventDefault();
  };

  node.dataset.dragHandle = '';
  node.addEventListener('pointerdown', down);
  node.addEventListener('touchstart', tstart, { passive: true });
  node.addEventListener('touchmove', blockScrollWhileDragging, { passive: false });
  node.addEventListener('dragstart', nativeDrag);
  node.addEventListener('contextmenu', ctx);
  return {
    update(n: DragOpts) {
      o = n;
    },
    destroy() {
      finish(false);
      node.removeEventListener('pointerdown', down);
      node.removeEventListener('touchstart', tstart);
      node.removeEventListener('touchmove', blockScrollWhileDragging);
      node.removeEventListener('dragstart', nativeDrag);
      node.removeEventListener('contextmenu', ctx);
    },
  };
}

/** Indicator class for an item while something is dragged over it. */
export function dropClass(id: string): string {
  const d = app.drag;
  if (!d) return '';
  if (d.id === id) return 'drag-source';
  const t = d.target;
  if (!t) return '';
  if (t.type === 'into') return id === 'g:' + t.group ? 'drop-into' : '';
  return t.id === id ? 'drop-' + t.type : '';
}
