<script lang="ts">
  import { onMount } from 'svelte';
  import { errorText } from '../../lib/api';
  import { timeAgo } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { notificationText } from '../../lib/notificationText';
  import {
    clearReadNotifications,
    markAllNotificationsRead,
    markNotificationRead,
    notifications,
    refreshNotifications,
    toast,
    unreadNotifications
  } from '../../lib/stores';
  import type { Notification } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';

  let loading = true;
  let busy = false;

  onMount(async () => {
    loading = true;
    await refreshNotifications();
    loading = false;
  });

  function icon(item: Notification): string {
    switch (item.type) {
      case 'upload_complete':
        return 'upload';
      case 'scan_complete':
        return 'shield';
      case 'share_expiring':
      case 'share_expired':
        return 'clock';
      case 'share_purged':
        return 'trash';
      case 'storage_warning':
        return 'harddrive';
      case 'security':
        return 'lock';
      default:
        return 'bell';
    }
  }

  async function markOne(item: Notification): Promise<void> {
    if (item.readAt) return;
    try {
      await markNotificationRead(item.id);
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function markAll(): Promise<void> {
    if ($unreadNotifications === 0) return;
    busy = true;
    try {
      await markAllNotificationsRead();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = false;
    }
  }

  async function clearRead(): Promise<void> {
    busy = true;
    try {
      await clearReadNotifications();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = false;
    }
  }
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <p class="text-sm text-slate-500 dark:text-slate-400">{t('notifications.unread', { count: $unreadNotifications })}</p>
    <div class="flex gap-2">
      <button type="button" class="btn-secondary btn-sm" disabled={busy || $unreadNotifications === 0} on:click={markAll}>
        <Icon name="check" size={15} />{t('notifications.markAll')}
      </button>
      <button type="button" class="btn-ghost btn-sm" disabled={busy} on:click={clearRead}>
        <Icon name="trash" size={15} />{t('notifications.clear')}
      </button>
    </div>
  </div>

  {#if loading}
    <div class="space-y-2">
      {#each Array(5) as _}
        <div class="skeleton h-16"></div>
      {/each}
    </div>
  {:else if $notifications.length === 0}
    <div class="card">
      <EmptyState icon="bell" title={t('notifications.empty')} message={t('activity.subtitle')} />
    </div>
  {:else}
    <div class="card">
      <ul class="divide-y divide-slate-100 dark:divide-slate-800">
        {#each $notifications as item (item.id)}
          <li>
            <button
              type="button"
              class="flex w-full items-start gap-3 px-5 py-4 text-left transition hover:bg-slate-50 dark:hover:bg-slate-800/50 {item.readAt ? '' : 'bg-brand-50/40 dark:bg-brand-500/5'}"
              on:click={() => markOne(item)}
            >
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl {item.readAt ? 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300' : 'bg-brand-100 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300'}">
                <Icon name={icon(item)} size={17} />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block text-sm text-slate-800 dark:text-slate-100">{notificationText(item)}</span>
                <span class="mt-0.5 block text-xs text-slate-400 dark:text-slate-500">{timeAgo(item.createdAt)}</span>
              </span>
              {#if !item.readAt}
                <span class="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-brand-500" aria-hidden="true"></span>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>
