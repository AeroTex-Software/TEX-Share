import { t } from './i18n';
import { formatDate } from './format';
import type { Notification } from './types';

function str(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

/** Human-readable notification text with a safe fallback per type. */
export function notificationText(item: Notification): string {
  const data = item.data || {};
  switch (item.type) {
    case 'upload_complete': {
      const name = str(data.filename) || str(data.slug) || str(data.title);
      return name ? t('notifications.upload_complete', { filename: name }) : t('profile.notify.upload_complete');
    }
    case 'scan_complete': {
      const name = str(data.filename) || t('common.name');
      return data.infected === true || str(data.status) === 'infected'
        ? t('notifications.scan_infected', { filename: name })
        : t('notifications.scan_clean', { filename: name });
    }
    case 'share_expiring':
      return t('notifications.share_expiring', {
        title: str(data.title) || str(data.slug) || t('common.name'),
        date: formatDate(num(data.expiresAt) || num(data.date))
      });
    case 'share_expired':
      return t('profile.notify.share_expired');
    case 'share_purged':
      return t('profile.notify.share_purged');
    case 'storage_warning':
      return t('notifications.storage_warning', { percent: Math.round(num(data.percent)) });
    case 'security': {
      const event = str(data.event);
      if (event === 'new_login') return t('notifications.new_login');
      if (event === 'password_changed') return t('activity.password_changed');
      if (event === '2fa_enabled') return t('activity.2fa_enabled');
      if (event === '2fa_disabled') return t('activity.2fa_disabled');
      if (event === 'account_suspended') return t('auth.accountSuspended');
      if (event === 'shares_received') return t('admin.users.transferTo');
      return t('profile.notify.security');
    }
    default:
      return t(`profile.notify.${item.type}`);
  }
}
