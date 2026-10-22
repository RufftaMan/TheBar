import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';
import { app } from './lib/store.svelte';

async function start() {
  // the server embeds the first payload as a JSON data block; in dev (vite) fetch it instead
  let init: any;
  try {
    const el = document.getElementById('init');
    if (el?.textContent) init = JSON.parse(el.textContent);
  } catch {
    /* fall through to fetching */
  }
  if (!init) {
    const res = await fetch('/api/init', { credentials: 'same-origin' });
    init = res.ok ? await res.json() : { auth: false };
  }
  app.load(init);
  mount(App, { target: document.getElementById('app')! });
}

start();
