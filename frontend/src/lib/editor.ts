// CodeMirror setup, loaded lazily with the Config tab.
import { EditorState, StateEffect, StateField } from '@codemirror/state';
import { Decoration, EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection } from '@codemirror/view';
import type { DecorationSet } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { yaml } from '@codemirror/lang-yaml';
import { HighlightStyle, syntaxHighlighting, indentOnInput, bracketMatching, foldGutter } from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { tags as t } from '@lezer/highlight';

const theme = EditorView.theme(
  {
    '&': { backgroundColor: '#090b0e', color: '#d7dbe2', fontSize: '12.5px', height: '100%' },
    '.cm-content': { fontFamily: 'var(--mono)', caretColor: '#f5a524' },
    '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '1.55' },
    '.cm-gutters': { backgroundColor: '#0d1014', color: '#4b5361', border: 'none', borderRight: '1px solid #1d222a' },
    '.cm-activeLine': { backgroundColor: '#11151b' },
    '.cm-activeLineGutter': { backgroundColor: '#11151b', color: '#9aa1ad' },
    '&.cm-focused .cm-cursor': { borderLeftColor: '#f5a524' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: '#2a3442 !important' },
    '.cm-panels': { backgroundColor: '#14171d', color: '#d7dbe2' },
    '.cm-panels input, .cm-panels button': { fontSize: '12px' },
    '.cm-searchMatch': { backgroundColor: 'rgba(245,165,36,.25)' },
    '.cm-flash': { backgroundColor: 'rgba(245,165,36,.18)', transition: 'background-color 1.5s' },
    '.cm-foldGutter span': { color: '#4b5361' },
  },
  { dark: true },
);

const highlight = HighlightStyle.define([
  { tag: [t.propertyName, t.definition(t.propertyName)], color: '#8fbcff' },
  { tag: [t.string, t.special(t.string)], color: '#b5d99c' },
  { tag: [t.number, t.bool, t.null], color: '#f0a96b' },
  { tag: t.comment, color: '#5f6876', fontStyle: 'italic' },
  { tag: [t.keyword, t.operator, t.punctuation, t.separator], color: '#9aa1ad' },
  { tag: [t.labelName, t.meta], color: '#d6a8f0' },
  { tag: t.variableName, color: '#ffd27a' },
]);

// a brief highlight on the line we jump to
const flashEffect = StateEffect.define<number | null>();
const flashField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes);
    for (const e of tr.effects) {
      if (e.is(flashEffect)) {
        if (e.value === null) return Decoration.none;
        const line = tr.state.doc.line(e.value);
        return Decoration.set([Decoration.line({ class: 'cm-flash' }).range(line.from)]);
      }
    }
    return deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

export interface Editor {
  get(): string;
  set(doc: string): void;
  goto(line: number): void;
  focus(): void;
  destroy(): void;
}

export function createEditor(parent: HTMLElement, doc: string, kind: 'compose' | 'env', onChange: (doc: string) => void): Editor {
  const extensions = [
    lineNumbers(),
    highlightActiveLineGutter(),
    foldGutter(),
    history(),
    drawSelection(),
    indentOnInput(),
    bracketMatching(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    search({ top: true }),
    keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap, ...searchKeymap]),
    syntaxHighlighting(highlight),
    theme,
    flashField,
    EditorState.tabSize.of(2),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) onChange(u.state.doc.toString());
    }),
  ];
  if (kind === 'compose') extensions.push(yaml());
  const view = new EditorView({ parent, state: EditorState.create({ doc, extensions }) });
  return {
    get: () => view.state.doc.toString(),
    set(d: string) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: d } });
    },
    goto(line: number) {
      const l = view.state.doc.line(Math.min(Math.max(1, line), view.state.doc.lines));
      view.dispatch({ selection: { anchor: l.from }, effects: flashEffect.of(l.number) });
      // scroll the editor's own scroller. EditorView.scrollIntoView would also
      // scroll the page, which makes the view jump when the Config tab opens.
      view.scrollDOM.scrollTop = Math.max(0, view.lineBlockAt(l.from).top - 40);
      setTimeout(() => view.dispatch({ effects: flashEffect.of(null) }), 1600);
    },
    focus: () => view.focus(),
    destroy: () => view.destroy(),
  };
}

/** Line numbers of each service under the top-level `services:` key. */
export function serviceLines(doc: string): Record<string, number> {
  const out: Record<string, number> = {};
  const lines = doc.split('\n');
  let inServices = false;
  let indent = -1;
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    if (!raw.trim() || raw.trim().startsWith('#')) continue;
    const lead = raw.length - raw.trimStart().length;
    if (lead === 0) {
      inServices = /^services\s*:\s*(#.*)?$/.test(raw);
      indent = -1;
      continue;
    }
    if (!inServices) continue;
    if (indent === -1) indent = lead;
    if (lead === indent) {
      const m = raw.trim().match(/^["']?([A-Za-z0-9._-]+)["']?\s*:/);
      if (m) out[m[1]] = i + 1;
    }
  }
  return out;
}
