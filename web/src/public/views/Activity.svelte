<script lang="ts">
  import { onMount } from 'svelte';
  import { errorText } from '../../lib/api';
  import { formatBytes, formatDateTime, timeAgo } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { loadActivity } from '../../lib/stores';
  import type { Activity } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';

  let items: Activity[] = [];
  let loading = true;
  let error = '';

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      items = await loadActivity(100);
    } catch (err) {
      error = errorText(err);
    } finally {
      loading = false;
    }
  }

  function label(item: Activity): string {
    const key = `activity.${item.type}`;
    const value = t(key);
    return value === key ? t('admin.common.unknownAction') : value;
  }

  function icon(item: Activity): string {
    switch (item.type) {
      case 'login':
        return 'logout';
      case 'download':
        return 'download';
      case 'upload_complete':
        return 'upload';
      case 'password_changed':
        return 'lock';
      case '2fa_enabled':
      case '2fa_disabled':
        return 'shield';
      default:
        return 'shares';
    }
  }

  function detail(item: Activity): string {
    const bytes = Number(item.meta?.bytes || 0);
    if (item.type === 'download' && bytes > 0) return formatBytes(bytes);
    const title = typeof item.meta?.title === 'string' ? item.meta.title : '';
    const slug = typeof item.meta?.slug === 'string' ? item.meta.slug : '';
    return title || slug || '';
  }
</script>

<div class="space-y-4">
  <div class="flex items-center justify-end">
    <button type="button" class="btn-secondary btn-sm" on:click={load}>
      <Icon name="refresh" size={15} />{t('common.refresh')}
    </button>
  </div>

  {#if loading}
    <div class="space-y-2">
      {#each Array(6) as _}
        <div class="skeleton h-16"></div>
      {/each}
    </div>
  {:else if error}
    <div class="card card-pad">
      <p class="flex items-center gap-2 text-sm text-rose-600 dark:text-rose-400"><Icon name="error" size={18} />{error}</p>
    </div>
  {:else if items.length === 0}
    <div class="card">
      <EmptyState icon="activity" title={t('activity.empty')} message={t('activity.subtitle')} />
    </div>
  {:else}
    <div class="card">
      <ul class="divide-y divide-slate-100 dark:divide-slate-800">
        {#each items as item (item.id)}
          <li class="flex items-center gap-3 px-5 py-3.5">
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
              <Icon name={icon(item)} size={17} />
            </span>
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium text-slate-800 dark:text-slate-100">{label(item)}</p>
              {#if detail(item)}
                <p class="truncate text-xs text-slate-500 dark:text-slate-400">{detail(item)}</p>
              {/if}
            </div>
            <div class="shrink-0 text-right">
              <p class="text-xs font-medium text-slate-600 dark:text-slate-300">{timeAgo(item.createdAt)}</p>
              <p class="text-[11px] text-slate-400 dark:text-slate-500">{formatDateTime(item.createdAt)}</p>
            </div>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>
