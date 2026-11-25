import { writable } from 'svelte/store';
import { api, mediaURL } from '../lib/api';
import { maintenanceMode, setupCompleted } from '../lib/stores';

export interface AdminMeta {
  appName: string;
  version: string;
  publicUrl: string;
  logoUrl: string;
}

export const adminMeta = writable<AdminMeta>({ appName: 'TEX Share', version: '', publicUrl: '', logoUrl: '' });

/** Loads the unauthenticated setup status used before any session exists. */
export async function loadAdminMeta(): Promise<void> {
  try {
    const res = await api<{ needsSetup: boolean; appName: string; version: string; maintenance: boolean }>('/setup/status', {
      auth: false
    });
    adminMeta.update((meta) => ({ ...meta, appName: res.appName || meta.appName, version: res.version || '' }));
    setupCompleted.set(!res.needsSetup);
    maintenanceMode.set(Boolean(res.maintenance));
  } catch {
    // Defaults remain; the session bootstrap surfaces hard failures.
  }
}

/** Refines branding and URLs once an authenticated admin session exists. */
export async function loadAdminShell(): Promise<void> {
  try {
    const res = await api<{
      system: { version: string; publicUrl?: string; adminUrl?: string };
      branding: { appName: string; logoUrl: string };
    }>('/settings');
    adminMeta.update((meta) => ({
      ...meta,
      appName: res.branding?.appName || meta.appName,
      version: res.system?.version || meta.version,
      publicUrl: res.system?.publicUrl || '',
      logoUrl: res.branding?.logoUrl ? mediaURL(res.branding.logoUrl) : ''
    }));
  } catch {
    // Branding is cosmetic; never block the shell.
  }
}
