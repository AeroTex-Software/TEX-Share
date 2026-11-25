import { t } from './i18n';
import type { Realm } from './types';

export class APIError extends Error {
  status: number;
  code: string;
  details?: string;

  constructor(status: number, code: string, message?: string) {
    super(message || code);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
    this.details = message;
  }
}

let realm: Realm = 'public';
let csrfToken = '';
let unauthorizedHandler: (() => void) | null = null;

export function configureAPI(next: Realm): void {
  realm = next;
}

export function currentRealm(): Realm {
  return realm;
}

export function setCSRF(token: string | undefined | null): void {
  csrfToken = token || '';
}

export function getCSRF(): string {
  return csrfToken;
}

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

export function apiPath(path: string): string {
  const p = path.startsWith('/') ? path : `/${path}`;
  return `/api/${realm}${p}`;
}

/**
 * The server returns public media URLs for avatars and branding. The admin
 * listener exposes equivalent aliases, so map them when running in admin.
 */
export function mediaURL(url: string | undefined | null): string {
  if (!url) return '';
  if (realm === 'admin') return url.replace('/api/public/', '/api/admin/');
  return url;
}

export function errorText(err: unknown): string {
  if (err instanceof APIError) {
    const key = `err.${err.code}`;
    const translated = t(key);
    if (translated !== key) return translated;
    if (err.details) return err.details;
    if (err.message && err.message !== err.code) return err.message;
    return t('common.error');
  }
  if (err instanceof Error && err.message) return err.message;
  return t('common.error');
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  auth?: boolean;
  keepalive?: boolean;
};

export async function api<T = unknown>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = (options.method || 'GET').toUpperCase();
  const headers: Record<string, string> = { Accept: 'application/json', ...(options.headers || {}) };
  let body: BodyInit | undefined;
  if (options.body !== undefined && options.body !== null) {
    if (
      options.body instanceof FormData ||
      options.body instanceof Blob ||
      options.body instanceof ArrayBuffer ||
      typeof options.body === 'string'
    ) {
      body = options.body as BodyInit;
    } else {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(options.body);
    }
  }
  if (method !== 'GET' && method !== 'HEAD' && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken;
  }
  let res: Response;
  try {
    res = await fetch(apiPath(path), {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: options.signal,
      keepalive: options.keepalive
    });
  } catch (err) {
    if ((err as Error)?.name === 'AbortError') throw err;
    throw new APIError(0, 'network_error', (err as Error)?.message);
  }
  if (res.status === 401 && options.auth !== false) {
    unauthorizedHandler?.();
  }
  if (!res.ok) {
    let code = `http_${res.status}`;
    let message = '';
    try {
      const data = await res.json();
      code = data?.error || code;
      message = data?.message || '';
    } catch {
      // Non-JSON error body.
    }
    throw new APIError(res.status, code, message);
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T;
  }
  const type = res.headers.get('content-type') || '';
  if (type.includes('application/json')) {
    return (await res.json()) as T;
  }
  return (await res.text()) as unknown as T;
}

function parseXHR<T>(xhr: XMLHttpRequest): T {
  if (xhr.status === 204 || !xhr.responseText) return undefined as T;
  try {
    return JSON.parse(xhr.responseText) as T;
  } catch {
    return xhr.responseText as unknown as T;
  }
}

function xhrError(xhr: XMLHttpRequest): APIError {
  let code = `http_${xhr.status}`;
  let message = '';
  try {
    const data = JSON.parse(xhr.responseText);
    code = data?.error || code;
    message = data?.message || '';
  } catch {
    // ignore
  }
  return new APIError(xhr.status, code, message);
}

export function xhrUpload<T = unknown>(
  method: string,
  path: string,
  body: Blob | File | FormData,
  onProgress?: (loaded: number, total: number) => void,
  signal?: AbortSignal,
  auth = true
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, apiPath(path), true);
    xhr.withCredentials = true;
    xhr.setRequestHeader('Accept', 'application/json');
    if (csrfToken) xhr.setRequestHeader('X-CSRF-Token', csrfToken);
    if (body instanceof File || body instanceof Blob) {
      xhr.setRequestHeader('Content-Type', 'application/octet-stream');
    }
    const abort = () => xhr.abort();
    if (signal) {
      if (signal.aborted) {
        reject(new DOMException('Aborted', 'AbortError'));
        return;
      }
      signal.addEventListener('abort', abort, { once: true });
    }
    const cleanup = () => {
      signal?.removeEventListener('abort', abort);
    };
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(event.loaded, event.total);
    };
    xhr.onerror = () => {
      cleanup();
      reject(new APIError(0, 'network_error'));
    };
    xhr.onabort = () => {
      cleanup();
      reject(new DOMException('Aborted', 'AbortError'));
    };
    xhr.onload = () => {
      cleanup();
      if (xhr.status === 401 && auth) unauthorizedHandler?.();
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(parseXHR<T>(xhr));
      } else {
        reject(xhrError(xhr));
      }
    };
    xhr.send(body);
  });
}

export async function fetchBlob(
  path: string,
  onProgress?: (loaded: number, total: number) => void,
  options: { signal?: AbortSignal; headers?: Record<string, string> } = {}
): Promise<Blob> {
  let res: Response;
  try {
    res = await fetch(apiPath(path), {
      credentials: 'same-origin',
      signal: options.signal,
      headers: { Accept: '*/*', ...(options.headers || {}) }
    });
  } catch (err) {
    if ((err as Error)?.name === 'AbortError') throw err;
    throw new APIError(0, 'network_error', (err as Error)?.message);
  }
  if (!res.ok) {
    let code = `http_${res.status}`;
    let message = '';
    try {
      const data = await res.json();
      code = data?.error || code;
      message = data?.message || '';
    } catch {
      // ignore
    }
    throw new APIError(res.status, code, message);
  }
  const total = Number(res.headers.get('content-length') || 0);
  if (!res.body) return new Blob([]);
  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let loaded = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) {
      chunks.push(value);
      loaded += value.byteLength;
      onProgress?.(loaded, total || loaded);
    }
  }
  return new Blob(chunks as BlobPart[], { type: res.headers.get('content-type') || 'application/octet-stream' });
}

export function downloadHref(path: string): string {
  return apiPath(path);
}

export async function copyText(value: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const area = document.createElement('textarea');
  area.value = value;
  area.style.position = 'fixed';
  area.style.opacity = '0';
  document.body.appendChild(area);
  area.select();
  document.execCommand('copy');
  area.remove();
}

export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename || 'download';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
