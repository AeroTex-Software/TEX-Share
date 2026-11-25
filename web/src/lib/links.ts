import { getOwnerShare } from './tokens';
import type { Share } from './types';

/** True when a path segment can only be a share token (43 base64url chars). */
export function isShareToken(segment: string): boolean {
  return segment.length >= 40 && /^[A-Za-z0-9_-]+$/.test(segment);
}

export function shareBaseUrl(domainHostname: string): string {
  const host = (domainHostname || '').trim();
  if (!host) return window.location.origin;
  const current = window.location.hostname;
  if (host.toLowerCase() === current.toLowerCase()) return window.location.origin;
  return `https://${host}`;
}

export function sharePathFor(share: Pick<Share, 'domainHostname' | 'urlPath'>): string {
  return `${shareBaseUrl(share.domainHostname)}${share.urlPath || ''}`;
}

export function ownerShareUrl(share: Pick<Share, 'id' | 'domainHostname' | 'urlPath'>): string {
  const token = getOwnerShare(share.id).token;
  if (!token) return '';
  return `${sharePathFor(share)}/${token}`;
}

export function withKeyFragment(url: string, keyFragment: string): string {
  if (!keyFragment) return url;
  return `${url}${keyFragment.startsWith('#') ? keyFragment : `#${keyFragment}`}`;
}

export function absoluteShareUrl(path: string, domainHostname: string): string {
  if (/^https?:\/\//i.test(path)) return path;
  return `${shareBaseUrl(domainHostname)}${path.startsWith('/') ? path : `/${path}`}`;
}

export function publicHostFromSystem(publicUrl?: string): string {
  if (!publicUrl) return window.location.host;
  try {
    return new URL(publicUrl).host;
  } catch {
    return publicUrl;
  }
}
