<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorText, mediaURL } from '../lib/api';
  import { formatBytes, formatDate, percent } from '../lib/format';
  import { locale, setLocale, t } from '../lib/i18n';
  import { patchUser, toast, user } from '../lib/stores';
  import { setTheme, theme } from '../lib/theme';
  import type { Language, Theme, User } from '../lib/types';
  import Avatar from './Avatar.svelte';
  import Icon from './Icon.svelte';
  import ProgressBar from './ProgressBar.svelte';
  import SecurityPanel from './SecurityPanel.svelte';
  import Toggle from './Toggle.svelte';

  export let allow2fa = true;

  interface ProfileResponse {
    user: User;
    limits: {
      storageLimitBytes: number;
      storageUsedBytes: number;
      storageReservedBytes: number;
    };
  }

  const notifyTypes: { key: string; label: string }[] = [
    { key: 'upload_complete', label: 'profile.notify.upload_complete' },
    { key: 'scan_complete', label: 'profile.notify.scan_complete' },
    { key: 'share_expiring', label: 'profile.notify.share_expiring' },
    { key: 'share_expired', label: 'profile.notify.share_expired' },
    { key: 'share_purged', label: 'profile.notify.share_purged' },
    { key: 'storage_warning', label: 'profile.notify.storage_warning' },
    { key: 'security', label: 'profile.notify.security' }
  ];

  const themes: { value: Theme; label: string }[] = [
    { value: 'system', label: 'common.theme.system' },
    { value: 'light', label: 'common.theme.light' },
    { value: 'dark', label: 'common.theme.dark' }
  ];

  const languages: { value: Language; label: string }[] = [
    { value: 'auto', label: 'common.default' },
    { value: 'de', label: 'Deutsch' },
    { value: 'en', label: 'English' }
  ];

  let loaded = false;
  let saving = false;
  let uploadingAvatar = false;
  let avatarInput: HTMLInputElement | null = null;

  let displayName = '';
  let language: Language = 'auto';
  let selectedTheme: Theme = 'system';
  let prefs: Record<string, boolean> = {};

  let storageLimit = 0;
  let storageUsed = 0;
  let storageReserved = 0;

  onMount(load);

  function applyProfile(u: User): void {
    displayName = u.displayName || '';
    language = u.language === 'de' || u.language === 'en' ? u.language : 'auto';
    selectedTheme = u.theme === 'light' || u.theme === 'dark' ? u.theme : 'system';
    prefs = { ...(u.notificationPrefs || {}) };
  }

  async function load(): Promise<void> {
    try {
      const res = await api<ProfileResponse>('/profile');
      applyProfile(res.user);
      storageLimit = res.limits.storageLimitBytes;
      storageUsed = res.limits.storageUsedBytes;
      storageReserved = res.limits.storageReservedBytes;
      loaded = true;
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  function prefValue(key: string): boolean {
    return prefs[key] !== false;
  }

  function togglePref(key: string, value: boolean): void {
    prefs = { ...prefs, [key]: value };
  }

  async function savePreferences(): Promise<void> {
    saving = true;
    try {
      const res = await api<{ user: User }>('/profile', {
        method: 'PATCH',
        body: { displayName, language, theme: selectedTheme, notificationPrefs: prefs }
      });
      applyProfile(res.user);
      patchUser(res.user);
      if (res.user.theme === 'light' || res.user.theme === 'dark' || res.user.theme === 'system') {
        setTheme(res.user.theme);
      }
      if (res.user.language === 'de' || res.user.language === 'en') {
        setLocale(res.user.language);
      }
      toast(t('common.saved'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      saving = false;
    }
  }

  async function onAvatarSelected(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) {
      toast(errorText({ code: 'file_too_large' } as never), 'error');
      return;
    }
    uploadingAvatar = true;
    try {
      const form = new FormData();
      form.append('file', file);
      const res = await api<{ user: User }>('/profile/avatar', { method: 'POST', body: form });
      patchUser(res.user);
      toast(t('common.saved'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      uploadingAvatar = false;
    }
  }

  async function removeAvatar(): Promise<void> {
    try {
      await api('/profile/avatar', { method: 'DELETE' });
      patchUser({ avatarUrl: '' });
      toast(t('common.saved'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  $: usedTotal = storageUsed + storageReserved;
  $: storagePercentValue = percent(usedTotal, storageLimit);
</script>

<div class="space-y-6">
  <!-- Identity -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
      <Icon name="user" size={19} />{t('profile.title')}
    </h2>
    <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('profile.subtitle')}</p>

    <div class="mt-5 flex flex-wrap items-center gap-5">
      <Avatar user={$user} size={72} />
      <div class="flex flex-col gap-2">
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-secondary btn-sm" disabled={uploadingAvatar} on:click={() => avatarInput?.click()}>
            {#if uploadingAvatar}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="upload" size={15} />{/if}
            {t('profile.avatarUpload')}
          </button>
          {#if $user?.avatarUrl}
            <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={removeAvatar}>
              <Icon name="trash" size={15} />{t('profile.avatarRemove')}
            </button>
          {/if}
        </div>
        <p class="text-xs text-slate-500 dark:text-slate-400">{t('profile.avatarHint')}</p>
        <input
          bind:this={avatarInput}
          type="file"
          accept="image/png,image/jpeg,image/webp"
          class="hidden"
          on:change={onAvatarSelected}
        />
      </div>
    </div>

    <div class="mt-6 grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="profile-username">{t('profile.username')}</label>
        <input id="profile-username" class="input" value={$user?.username || ''} disabled />
      </div>
      <div>
        <label class="label" for="profile-role">{t('profile.role')}</label>
        <input
          id="profile-role"
          class="input"
          value={$user?.role === 'admin' ? t('profile.role.admin') : t('profile.role.user')}
          disabled
        />
      </div>
      <div class="sm:col-span-2">
        <label class="label" for="profile-display">{t('profile.displayName')}</label>
        <input id="profile-display" class="input" maxlength="80" bind:value={displayName} />
      </div>
      <div>
        <label class="label" for="profile-language">{t('common.language')}</label>
        <select id="profile-language" class="input" bind:value={language}>
          {#each languages as item}
            <option value={item.value}>{item.label.startsWith('common.') ? t(item.label) : item.label}</option>
          {/each}
        </select>
        <p class="hint">{t('profile.languageHint')}</p>
      </div>
      <div>
        <label class="label" for="profile-theme">{t('common.theme')}</label>
        <select id="profile-theme" class="input" bind:value={selectedTheme}>
          {#each themes as item}
            <option value={item.value}>{t(item.label)}</option>
          {/each}
        </select>
        <p class="hint">{t('profile.themeHint')}</p>
      </div>
    </div>
  </section>

  <!-- Storage -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
      <Icon name="harddrive" size={19} />{t('profile.storage')}
    </h2>
    <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('profile.storageHint')}</p>
    <div class="mt-5">
      {#if storageLimit > 0}
        <ProgressBar
          value={usedTotal}
          max={storageLimit}
          label={`${formatBytes(usedTotal)} ${t('common.of')} ${formatBytes(storageLimit)}`}
          tone={storagePercentValue >= 90 ? 'rose' : storagePercentValue >= 75 ? 'amber' : 'brand'}
        />
        {#if storageReserved > 0}
          <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">
            {t('common.reserved')}: {formatBytes(storageReserved)}
          </p>
        {/if}
      {:else}
        <p class="text-sm font-medium text-slate-700 dark:text-slate-200">
          {t('common.unlimited')} · {formatBytes(usedTotal)} {t('common.used')}
        </p>
      {/if}
      <p class="mt-3 text-xs text-slate-500 dark:text-slate-400">
        {t('profile.memberSince')}: {formatDate($user?.createdAt)}
      </p>
    </div>
  </section>

  <!-- Notifications -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
      <Icon name="bell" size={19} />{t('profile.notifications')}
    </h2>
    <div class="mt-4 space-y-3">
      {#each notifyTypes as item}
        <Toggle
          checked={prefValue(item.key)}
          label={t(item.label)}
          id={`pref-${item.key}`}
          on:click={() => togglePref(item.key, !prefValue(item.key))}
        />
      {/each}
    </div>
  </section>

  <div class="flex justify-end">
    <button type="button" class="btn-primary" disabled={saving || !loaded} on:click={savePreferences}>
      {#if saving}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
      {t('common.save')}
    </button>
  </div>

  <!-- Security -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
      <Icon name="shield" size={19} />{t('profile.security')}
    </h2>
    <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('profile.securityHint')}</p>
  </section>

  <SecurityPanel totpEnabled={$user?.totpEnabled || false} {allow2fa} />
</div>
