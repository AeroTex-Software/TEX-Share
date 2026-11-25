import { derived, get, writable } from 'svelte/store';
import { api, APIError, setCSRF, setUnauthorizedHandler } from './api';
import { locale } from './i18n';
import { t } from './i18n';
import { navigate } from './router';
import { theme } from './theme';
import type { Activity, Language, Notification, PublicMeta, Theme, User } from './types';

/* ------------------------------------------------------------------------- */
/* Session                                                                    */
/* ------------------------------------------------------------------------- */

export interface AuthMe {
  user: User;
  csrf: string;
  realm: string;
  mustChangePassword: boolean;
  setupCompleted: boolean;
  maintenance: boolean;
}

export interface LoginResult {
  ok?: boolean;
  totpRequired?: boolean;
  challenge?: string;
  user?: User;
  csrf?: string;
}

export const user = writable<User | null>(null);
export const sessionReady = writable(false);
export const sessionError = writable('');
export const mustChangePassword = writable(false);
export const maintenanceMode = writable(false);
export const setupCompleted = writable(true);

export const authenticated = derived(user, ($user) => $user !== null);

let bootstrapPromise: Promise<User | null> | null = null;

function applyPreferences(u: User): void {
  if (u.theme === 'system' || u.theme === 'light' || u.theme === 'dark') {
    theme.set(u.theme as Theme);
  }
  if (u.language === 'de' || u.language === 'en') {
    locale.set(u.language as 'de' | 'en');
  }
}

function acceptUser(u: User): void {
  user.set(u);
  mustChangePassword.set(Boolean(u.mustChangePassword));
  applyPreferences(u);
}

/** Loads the current session once. A 401 is a normal signed-out state. */
export function bootstrapSession(): Promise<User | null> {
  if (bootstrapPromise) return bootstrapPromise;
  bootstrapPromise = (async () => {
    try {
      const res = await api<AuthMe>('/auth/me', { auth: false });
      setCSRF(res.csrf);
      acceptUser(res.user);
      mustChangePassword.set(Boolean(res.mustChangePassword));
      maintenanceMode.set(Boolean(res.maintenance));
      setupCompleted.set(res.setupCompleted !== false);
      return res.user;
    } catch (err) {
      if (err instanceof APIError && err.status === 401) return null;
      if (err instanceof APIError && err.code === 'setup_required') {
        setupCompleted.set(false);
        return null;
      }
      sessionError.set(err instanceof Error ? err.message : String(err));
      return null;
    } finally {
      sessionReady.set(true);
    }
  })();
  return bootstrapPromise;
}

export function resetSessionBootstrap(): void {
  bootstrapPromise = null;
  sessionReady.set(false);
}

export async function login(username: string, password: string, remember: boolean): Promise<LoginResult> {
  const res = await api<LoginResult>('/auth/login', {
    method: 'POST',
    auth: false,
    body: { username, password, remember }
  });
  if (res.ok && res.user) {
    setCSRF(res.csrf);
    acceptUser(res.user);
  }
  return res;
}

export async function verifySecondFactor(challenge: string, code: string, recovery: boolean): Promise<LoginResult> {
  const res = await api<LoginResult>('/auth/2fa', {
    method: 'POST',
    auth: false,
    body: { challenge, code, recovery }
  });
  if (res.ok && res.user) {
    setCSRF(res.csrf);
    acceptUser(res.user);
  }
  return res;
}

export async function logout(loginPath = '/login'): Promise<void> {
  try {
    await api('/auth/logout', { method: 'POST', body: {} });
  } catch {
    // The session may already be gone; clearing locally is still correct.
  }
  clearSession();
  navigate(loginPath, true);
}

export function clearSession(): void {
  user.set(null);
  mustChangePassword.set(false);
  setCSRF('');
}

export async function refreshUser(): Promise<User | null> {
  try {
    const res = await api<AuthMe>('/auth/me');
    setCSRF(res.csrf);
    acceptUser(res.user);
    mustChangePassword.set(Boolean(res.mustChangePassword));
    maintenanceMode.set(Boolean(res.maintenance));
    return res.user;
  } catch {
    return null;
  }
}

export function patchUser(patch: Partial<User>): void {
  user.update((current) => (current ? { ...current, ...patch } : current));
  if (patch.mustChangePassword !== undefined) mustChangePassword.set(Boolean(patch.mustChangePassword));
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  const res = await api<{ ok: boolean; csrf: string }>('/auth/change-password', {
    method: 'POST',
    body: { currentPassword, newPassword }
  });
  setCSRF(res.csrf);
  patchUser({ mustChangePassword: false });
  mustChangePassword.set(false);
}

/**
 * Installs the global 401 handler. Only an authenticated user is redirected:
 * signed-out visitors on public share pages must never be bounced to login.
 */
export function installUnauthorizedHandler(loginPath: string): void {
  setUnauthorizedHandler(() => {
    const hadUser = get(user) !== null;
    clearSession();
    if (hadUser) {
      toast(t('auth.sessionExpired'), 'error');
      navigate(loginPath, true);
    }
  });
}

/* ------------------------------------------------------------------------- */
/* Public metadata                                                            */
/* ------------------------------------------------------------------------- */

export const publicMeta = writable<PublicMeta | null>(null);

export async function loadPublicMeta(): Promise<PublicMeta | null> {
  try {
    const meta = await api<PublicMeta>('/meta', { auth: false });
    publicMeta.set(meta);
    maintenanceMode.set(Boolean(meta.maintenance));
    setupCompleted.set(!meta.setupNeeded);
    if (meta.appName) document.title = meta.appName;
    return meta;
  } catch {
    return null;
  }
}

/* ------------------------------------------------------------------------- */
/* Authenticated public configuration                                         */
/* ------------------------------------------------------------------------- */

export interface PublicLimits {
  maxFileSizeBytes: number;
  maxShareSizeBytes: number;
  chunkSize: number;
  resumableThreshold: number;
  sharePasswordMin: number;
  defaultRetentionDays: number;
  storageLimitBytes: number;
  storageUsedBytes: number;
  storageReservedBytes: number;
}

export interface PublicConfig {
  user: User;
  domains: import('./types').Domain[];
  limits: PublicLimits;
  adminUrl: string;
}

export const publicConfig = writable<PublicConfig | null>(null);

export async function loadPublicConfig(): Promise<PublicConfig | null> {
  try {
    const cfg = await api<PublicConfig>('/config');
    publicConfig.set(cfg);
    if (cfg.user) acceptUser(cfg.user);
    return cfg;
  } catch {
    return null;
  }
}

/* ------------------------------------------------------------------------- */
/* Toasts                                                                     */
/* ------------------------------------------------------------------------- */

export type ToastKind = 'success' | 'error' | 'info';

export interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

let toastSeq = 0;
export const toasts = writable<ToastItem[]>([]);

export function dismissToast(id: number): void {
  toasts.update((list) => list.filter((item) => item.id !== id));
}

export function toast(message: string, kind: ToastKind = 'success', timeout = 5200): number {
  const id = ++toastSeq;
  toasts.update((list) => [...list.slice(-4), { id, kind, message }]);
  if (timeout > 0) {
    window.setTimeout(() => dismissToast(id), timeout);
  }
  return id;
}

/* ------------------------------------------------------------------------- */
/* Confirmation dialogs                                                       */
/* ------------------------------------------------------------------------- */

export interface ConfirmOptions {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  requireText?: string;
  requireTextHint?: string;
}

interface ConfirmRequest extends ConfirmOptions {
  resolve: (value: boolean) => void;
}

export const confirmRequest = writable<ConfirmRequest | null>(null);

export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    confirmRequest.set({ ...options, resolve });
  });
}

export function resolveConfirm(value: boolean): void {
  const current = get(confirmRequest);
  if (!current) return;
  confirmRequest.set(null);
  current.resolve(value);
}

/* ------------------------------------------------------------------------- */
/* Notifications                                                              */
/* ------------------------------------------------------------------------- */

export const notifications = writable<Notification[]>([]);
export const unreadNotifications = writable(0);

export async function refreshNotifications(): Promise<void> {
  try {
    const res = await api<{ notifications: Notification[]; unread: number }>('/notifications?limit=40');
    notifications.set(res.notifications || []);
    unreadNotifications.set(res.unread || 0);
  } catch {
    // Notification refresh must never break a page.
  }
}

/** Alias kept for view code that predates the rename. */
export const loadNotifications = refreshNotifications;

export async function markNotificationRead(id: number): Promise<void> {
  await api('/notifications/read', { method: 'POST', body: { ids: [id] } });
  notifications.update((list) =>
    list.map((item) => (item.id === id && !item.readAt ? { ...item, readAt: Math.floor(Date.now() / 1000) } : item))
  );
  unreadNotifications.update((n) => Math.max(0, n - 1));
}

export async function markAllNotificationsRead(): Promise<void> {
  await api('/notifications/read', { method: 'POST', body: { all: true } });
  const now = Math.floor(Date.now() / 1000);
  notifications.update((list) => list.map((item) => ({ ...item, readAt: item.readAt || now })));
  unreadNotifications.set(0);
}

export async function clearReadNotifications(): Promise<void> {
  await api('/notifications', { method: 'DELETE' });
  notifications.update((list) => list.filter((item) => !item.readAt));
}

/* ------------------------------------------------------------------------- */
/* Activity                                                                   */
/* ------------------------------------------------------------------------- */

export async function loadActivity(limit = 50): Promise<Activity[]> {
  const res = await api<{ activity: Activity[] }>(`/activity?limit=${limit}`);
  return res.activity || [];
}

/* ------------------------------------------------------------------------- */
/* Helpers                                                                    */
/* ------------------------------------------------------------------------- */

export function languageValue(value: Language): 'auto' | 'de' | 'en' {
  return value === 'de' || value === 'en' ? value : 'auto';
}
