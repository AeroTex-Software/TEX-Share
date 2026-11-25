import { writable } from 'svelte/store';

export interface RouteState {
  path: string;
  segments: string[];
  query: URLSearchParams;
  hash: string;
}

function parse(): RouteState {
  const path = window.location.pathname.replace(/\/+$/, '') || '/';
  return {
    path,
    segments: path.split('/').filter(Boolean),
    query: new URLSearchParams(window.location.search),
    hash: window.location.hash
  };
}

export const route = writable<RouteState>(parse());

function publish(): void {
  route.set(parse());
}

export function navigate(to: string, replace = false): void {
  const url = new URL(to, window.location.origin);
  if (url.origin !== window.location.origin) {
    window.location.href = to;
    return;
  }
  const target = url.pathname + url.search + url.hash;
  if (target === window.location.pathname + window.location.search + window.location.hash) {
    publish();
    return;
  }
  if (replace) history.replaceState({}, '', target);
  else history.pushState({}, '', target);
  publish();
  window.scrollTo({ top: 0, behavior: 'instant' as ScrollBehavior });
}

export function replaceQuery(params: Record<string, string | number | null | undefined>): void {
  const url = new URL(window.location.href);
  for (const [key, value] of Object.entries(params)) {
    if (value === null || value === undefined || value === '') url.searchParams.delete(key);
    else url.searchParams.set(key, String(value));
  }
  history.replaceState({}, '', url.pathname + url.search + url.hash);
  publish();
}

export function startRouter(): void {
  window.addEventListener('popstate', publish);
  document.addEventListener('click', (event) => {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const anchor = (event.target as HTMLElement | null)?.closest('a');
    if (!anchor) return;
    const href = anchor.getAttribute('href');
    if (!href || href.startsWith('#') || anchor.target === '_blank' || anchor.hasAttribute('download')) return;
    if (anchor.dataset.native !== undefined) return;
    const url = new URL(href, window.location.origin);
    if (url.origin !== window.location.origin) return;
    event.preventDefault();
    navigate(url.pathname + url.search + url.hash);
  });
}

export function isActive(path: string, prefix = false): boolean {
  let current = '/';
  route.subscribe((r) => (current = r.path))();
  if (prefix) return current === path || current.startsWith(`${path}/`);
  return current === path;
}
