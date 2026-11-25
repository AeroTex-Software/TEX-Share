import { writable } from 'svelte/store';
import type { Theme } from './types';

const STORAGE_KEY = 'texshare.theme';

function initial(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
}

export const theme = writable<Theme>(initial());

const media = window.matchMedia('(prefers-color-scheme: dark)');

function apply(value: Theme): void {
  const dark = value === 'dark' || (value === 'system' && media.matches);
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  document.documentElement.dataset.theme = value;
}

theme.subscribe((value) => {
  localStorage.setItem(STORAGE_KEY, value);
  apply(value);
});

media.addEventListener('change', () => {
  let value: Theme = 'system';
  theme.subscribe((v) => (value = v))();
  if (value === 'system') apply(value);
});

export function setTheme(value: Theme): void {
  theme.set(value);
}

export function cycleTheme(): void {
  theme.update((value) => (value === 'system' ? 'light' : value === 'light' ? 'dark' : 'system'));
}
