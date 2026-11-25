export function formatBytes(value: number | undefined | null, digits = 1): string {
  const n = Number(value || 0);
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const exp = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  const num = n / Math.pow(1024, exp);
  const d = exp === 0 ? 0 : digits;
  return `${num.toFixed(d).replace(/\.0+$/, '')} ${units[exp]}`;
}

export function formatNumber(value: number | undefined | null): string {
  return new Intl.NumberFormat(docLang()).format(Number(value || 0));
}

export function docLang(): string | undefined {
  const lang = typeof document !== 'undefined' ? document.documentElement.lang : '';
  return lang || undefined;
}

export function formatDate(unix?: number | null): string {
  if (!unix) return '—';
  return new Intl.DateTimeFormat(docLang(), { dateStyle: 'medium' }).format(new Date(unix * 1000));
}

export function formatDateTime(unix?: number | null): string {
  if (!unix) return '—';
  return new Intl.DateTimeFormat(docLang(), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(unix * 1000));
}

export function formatTime(unix?: number | null): string {
  if (!unix) return '—';
  return new Intl.DateTimeFormat(docLang(), { timeStyle: 'short' }).format(new Date(unix * 1000));
}

export function timeAgo(unix?: number | null): string {
  if (!unix) return '—';
  const diff = Math.round(unix - Date.now() / 1000);
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(docLang(), { numeric: 'auto' });
  if (abs < 60) return rtf.format(diff, 'second');
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
  if (abs < 2592000) return rtf.format(Math.round(diff / 86400), 'day');
  if (abs < 31536000) return rtf.format(Math.round(diff / 2592000), 'month');
  return rtf.format(Math.round(diff / 31536000), 'year');
}

export function formatDuration(seconds?: number | null): string {
  const s = Math.max(0, Math.floor(Number(seconds || 0)));
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${s}s`;
}

export function toDatetimeLocal(unix?: number | null): string {
  if (!unix) return '';
  const d = new Date(unix * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromDatetimeLocal(value: string): number {
  if (!value) return 0;
  const t = new Date(value).getTime();
  return Number.isFinite(t) ? Math.floor(t / 1000) : 0;
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

export function percent(used: number, total: number): number {
  if (!total || total <= 0) return 0;
  return clamp((used / total) * 100, 0, 100);
}

export function plural(count: number, singular: string, pluralForm: string): string {
  return count === 1 ? singular : pluralForm;
}

export function initials(name: string): string {
  return (name || '?')
    .split(/[\s._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() || '')
    .join('');
}

export function truncate(value: string, max: number): string {
  if (!value) return '';
  return value.length > max ? `${value.slice(0, Math.max(0, max - 1))}…` : value;
}
