<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { APIError, api, copyText, errorText, mediaURL } from '../../lib/api';
  import { t } from '../../lib/i18n';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { SystemInfo } from '../../lib/types';
  import Icon from '../../components/Icon.svelte';
  import Toggle from '../../components/Toggle.svelte';

  interface SettingsResponse {
    settings: Record<string, string>;
    backup: { schedule: string; hour: number; retention: number; directory: string; encrypt: boolean };
    system: SystemInfo;
    branding: { appName: string; logo: boolean; favicon: boolean; logoUrl: string; faviconUrl: string };
  }

  interface CloudflareResponse {
    cloudflare: {
      connected: boolean;
      enabled: boolean;
      available: boolean;
      availableError?: string;
      loginState: string;
      loginUrl: string;
      loginError?: string;
      tunnelId: string;
      tunnelName: string;
      hostname: string;
    };
    tunnel: { enabled: boolean; status: string; message?: string; pid?: number; startedAt?: number; restarts?: number };
  }

  const GIB = 1024 * 1024 * 1024;
  const MIB = 1024 * 1024;

  let loading = true;
  let saving = '';
  let error = '';
  let raw: Record<string, string> = {};
  let branding: SettingsResponse['branding'] = { appName: '', logo: false, favicon: false, logoUrl: '', faviconUrl: '' };
  let system: SystemInfo | null = null;

  // Branding
  let appName = '';
  let uploadBusy = '';

  type BrandingKind = 'logo' | 'favicon';
  // A selected file is previewed locally and only uploaded when the admin
  // saves, so a custom logo never replaces the bundled default by accident.
  let pending: Record<BrandingKind, File | null> = { logo: null, favicon: null };
  let previews: Record<BrandingKind, string> = { logo: '', favicon: '' };

  $: brandingItems = [
    {
      kind: 'logo' as BrandingKind,
      label: t('admin.settings.logo'),
      has: branding.logo,
      url: branding.logoUrl,
      fallback: '/assets/tex-share-icon.png',
      accept: 'image/png,image/jpeg,image/webp,image/svg+xml'
    },
    {
      kind: 'favicon' as BrandingKind,
      label: t('admin.settings.favicon'),
      has: branding.favicon,
      url: branding.faviconUrl,
      fallback: '/favicon.png',
      accept: 'image/png,image/x-icon,image/vnd.microsoft.icon,image/jpeg'
    }
  ];

  // General
  let publicBaseUrl = '';
  let forceHttpsLinks = false;
  let requireDomainVerification = false;
  let allow2fa = true;
  let monitoringRefreshSeconds = 5;
  let defaultShareRetentionDays = 0;
  let maintenanceMode = false;

  // Storage
  let defaultStorageGb = 0;
  let globalStorageGb = 0;
  let allowUnlimitedStorage = false;
  let maxFileMb = 0;
  let maxShareMb = 0;
  let resumableMb = 0;
  let trashRetentionDays = 0;
  let statsRetentionDays = 0;

  // Security
  let maxSessionHours = 0;
  let rememberMeDays = 0;
  let shareAccessHours = 0;
  let passwordMinLength = 8;
  let loginRatePerMinute = 0;
  let loginFailuresBeforeLock = 0;
  let lockoutMinutes = 0;
  let apiRatePerMinute = 0;
  let twofaAttempts = 0;

  // Uploads
  let uploadConcurrencyPerUser = 0;
  let downloadConcurrencyPerUser = 0;
  let globalUploadLimit = 0;
  let globalDownloadLimit = 0;
  let zipConcurrency = 0;
  let uploadQueueLimit = 0;
  let blockedExtensions = '';
  let blockedMime = '';

  // Antivirus
  let antivirusEnabled = false;
  let freshclamIntervalHours = 0;

  // Backups
  let backupSchedule = 'off';
  let backupHour = 3;
  let backupRetention = 14;
  let backupPath = '';
  let backupEncryptAuto = true;
  let backupDirectory = '';

  // Cloudflare
  let cf: CloudflareResponse | null = null;
  let cfTunnelName = '';
  let cfProvisionHost = '';
  let cfBusy = '';
  let cfPollTimer: ReturnType<typeof setInterval> | null = null;

  onMount(load);
  onDestroy(() => {
    if (cfPollTimer) clearInterval(cfPollTimer);
    clearPreviews();
  });

  function bool(key: string): boolean {
    const v = (raw[key] || '').toLowerCase();
    return v === '1' || v === 'true';
  }

  function int(key: string, fallback = 0): number {
    const n = Number.parseInt(raw[key] || '', 10);
    return Number.isFinite(n) ? n : fallback;
  }

  function parseList(key: string): string {
    try {
      const arr = JSON.parse(raw[key] || '[]');
      return Array.isArray(arr) ? arr.join(', ') : '';
    } catch {
      return '';
    }
  }

  function toList(value: string): string {
    const arr = value
      .split(/[\n,]+/)
      .map((v) => v.trim())
      .filter(Boolean);
    return JSON.stringify(arr);
  }

  function b2gb(bytes: number): number {
    return Math.round((bytes || 0) / GIB);
  }

  function b2mb(bytes: number): number {
    return Math.round((bytes || 0) / MIB);
  }

  function gb2b(gb: number): number {
    return Math.max(0, Math.round((Number(gb) || 0) * GIB));
  }

  function mb2b(mb: number): number {
    return Math.max(0, Math.round((Number(mb) || 0) * MIB));
  }

  async function load(): Promise<void> {
    loading = true;
    try {
      const res = await api<SettingsResponse>('/settings');
      raw = res.settings || {};
      branding = res.branding;
      system = res.system;
      appName = branding.appName || raw['app_name'] || '';

      publicBaseUrl = raw['public_base_url'] || '';
      forceHttpsLinks = bool('force_https_links');
      requireDomainVerification = bool('require_domain_verification');
      allow2fa = bool('allow_2fa');
      monitoringRefreshSeconds = int('monitoring_refresh_seconds', 5);
      defaultShareRetentionDays = int('default_share_retention_days');
      maintenanceMode = bool('maintenance_mode');

      defaultStorageGb = b2gb(int('default_storage_limit_bytes'));
      globalStorageGb = b2gb(int('global_storage_limit_bytes'));
      allowUnlimitedStorage = bool('allow_unlimited_storage');
      maxFileMb = b2mb(int('max_file_size_bytes'));
      maxShareMb = b2mb(int('max_share_size_bytes'));
      resumableMb = b2mb(int('resumable_threshold_bytes'));
      trashRetentionDays = int('trash_retention_days');
      statsRetentionDays = int('stats_retention_days', 90);

      maxSessionHours = int('max_session_hours', 168);
      rememberMeDays = int('remember_me_days', 30);
      shareAccessHours = int('share_access_hours', 24);
      passwordMinLength = int('password_min_length', 8);
      loginRatePerMinute = int('login_rate_per_minute', 10);
      loginFailuresBeforeLock = int('login_failures_before_lock', 5);
      lockoutMinutes = int('lockout_minutes', 15);
      apiRatePerMinute = int('api_rate_per_minute', 600);
      twofaAttempts = int('twofa_attempts', 5);

      uploadConcurrencyPerUser = int('upload_concurrency_per_user', 3);
      downloadConcurrencyPerUser = int('download_concurrency_per_user', 5);
      globalUploadLimit = int('global_upload_limit', 8);
      globalDownloadLimit = int('global_download_limit', 50);
      zipConcurrency = int('zip_concurrency', 2);
      uploadQueueLimit = int('upload_queue_limit', 10);
      blockedExtensions = parseList('blocked_extensions');
      blockedMime = parseList('blocked_mime');

      antivirusEnabled = bool('antivirus_enabled');
      freshclamIntervalHours = int('freshclam_interval_hours', 24);

      backupSchedule = raw['backup_schedule'] || 'off';
      backupHour = int('backup_hour', 3);
      backupRetention = int('backup_retention_count', 14);
      backupPath = raw['backup_path'] || '';
      backupEncryptAuto = bool('backup_encrypt_auto');
      backupDirectory = res.backup?.directory || '';

      await loadCloudflare();
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      loading = false;
    }
  }

  async function loadCloudflare(): Promise<void> {
    try {
      cf = await api<CloudflareResponse>('/cloudflare');
      cfTunnelName = cf.cloudflare.tunnelName || 'tex-share';
      cfProvisionHost = cf.cloudflare.hostname || '';
      syncCfPoll();
    } catch {
      // Cloudflare is optional.
    }
  }

  // syncCfPoll keeps polling while the browser login is pending so the UI
  // switches to "connected" automatically once Cloudflare redirects back.
  function syncCfPoll(): void {
    const waiting = cf?.cloudflare.loginState === 'waiting';
    if (waiting && !cfPollTimer) {
      cfPollTimer = setInterval(() => void loadCloudflare(), 2000);
    } else if (!waiting && cfPollTimer) {
      clearInterval(cfPollTimer);
      cfPollTimer = null;
    }
  }

  async function saveSection(name: string, map: Record<string, string>): Promise<void> {
    saving = name;
    error = '';
    try {
      await api('/settings', { method: 'PATCH', body: { settings: map } });
      toast(t('admin.settings.saved'), 'success');
      await load();
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      saving = '';
    }
  }

  function saveBranding(): void {
    void persistBranding();
  }

  // selectBranding stages a file locally and shows an immediate preview; the
  // upload only happens when the admin saves.
  function selectBranding(kind: BrandingKind, event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0] || null;
    input.value = '';
    if (!file) return;
    if (previews[kind]) URL.revokeObjectURL(previews[kind]);
    pending = { ...pending, [kind]: file };
    previews = { ...previews, [kind]: URL.createObjectURL(file) };
  }

  function discardPreview(kind: BrandingKind): void {
    if (previews[kind]) URL.revokeObjectURL(previews[kind]);
    pending = { ...pending, [kind]: null };
    previews = { ...previews, [kind]: '' };
  }

  function clearPreviews(): void {
    (['logo', 'favicon'] as BrandingKind[]).forEach((kind) => {
      if (previews[kind]) URL.revokeObjectURL(previews[kind]);
    });
    previews = { logo: '', favicon: '' };
    pending = { logo: null, favicon: null };
  }

  async function persistBranding(): Promise<void> {
    saving = 'branding';
    error = '';
    try {
      for (const kind of ['logo', 'favicon'] as BrandingKind[]) {
        const file = pending[kind];
        if (!file) continue;
        const fd = new FormData();
        fd.append('file', file);
        await api(`/branding/${kind}`, { method: 'POST', body: fd });
      }
      await api('/settings', { method: 'PATCH', body: { settings: { app_name: appName.trim() } } });
      clearPreviews();
      toast(t('admin.settings.saved'), 'success');
      await load();
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      saving = '';
    }
  }

  function saveGeneral(): void {
    void saveSection('general', {
      public_base_url: publicBaseUrl.trim(),
      force_https_links: forceHttpsLinks ? '1' : '0',
      require_domain_verification: requireDomainVerification ? '1' : '0',
      allow_2fa: allow2fa ? '1' : '0',
      monitoring_refresh_seconds: String(monitoringRefreshSeconds),
      default_share_retention_days: String(defaultShareRetentionDays)
    });
  }

  function saveStorage(): void {
    void saveSection('storage', {
      default_storage_limit_bytes: String(gb2b(defaultStorageGb)),
      global_storage_limit_bytes: String(gb2b(globalStorageGb)),
      allow_unlimited_storage: allowUnlimitedStorage ? '1' : '0',
      max_file_size_bytes: String(mb2b(maxFileMb)),
      max_share_size_bytes: String(mb2b(maxShareMb)),
      resumable_threshold_bytes: String(mb2b(resumableMb)),
      trash_retention_days: String(trashRetentionDays),
      stats_retention_days: String(statsRetentionDays)
    });
  }

  function saveSecurity(): void {
    void saveSection('security', {
      max_session_hours: String(maxSessionHours),
      remember_me_days: String(rememberMeDays),
      share_access_hours: String(shareAccessHours),
      password_min_length: String(passwordMinLength),
      login_rate_per_minute: String(loginRatePerMinute),
      login_failures_before_lock: String(loginFailuresBeforeLock),
      lockout_minutes: String(lockoutMinutes),
      api_rate_per_minute: String(apiRatePerMinute),
      twofa_attempts: String(twofaAttempts)
    });
  }

  function saveUploads(): void {
    void saveSection('uploads', {
      upload_concurrency_per_user: String(uploadConcurrencyPerUser),
      download_concurrency_per_user: String(downloadConcurrencyPerUser),
      global_upload_limit: String(globalUploadLimit),
      global_download_limit: String(globalDownloadLimit),
      zip_concurrency: String(zipConcurrency),
      upload_queue_limit: String(uploadQueueLimit),
      blocked_extensions: toList(blockedExtensions),
      blocked_mime: toList(blockedMime)
    });
  }

  function saveAntivirus(): void {
    void saveSection('antivirus', {
      antivirus_enabled: antivirusEnabled ? '1' : '0',
      freshclam_interval_hours: String(freshclamIntervalHours)
    });
  }

  function saveBackups(): void {
    void saveSection('backups', {
      backup_schedule: backupSchedule,
      backup_hour: String(backupHour),
      backup_retention_count: String(backupRetention),
      backup_path: backupPath.trim(),
      backup_encrypt_auto: backupEncryptAuto ? '1' : '0'
    });
  }

  async function setMaintenance(next: boolean): Promise<void> {
    maintenanceMode = next;
    await saveSection('maintenance', { maintenance_mode: next ? '1' : '0' });
  }

  async function removeBranding(kind: 'logo' | 'favicon'): Promise<void> {
    uploadBusy = kind;
    try {
      await api(`/branding/${kind}`, { method: 'DELETE' });
      toast(t('admin.settings.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      uploadBusy = '';
    }
  }

  async function cfLogin(): Promise<void> {
    cfBusy = 'login';
    try {
      await api('/cloudflare/login', { method: 'POST', body: {} });
      await loadCloudflare();
    } catch (err) {
      toast(cfError(err), 'error');
    } finally {
      cfBusy = '';
    }
  }

  async function cfCancelLogin(): Promise<void> {
    cfBusy = 'cancel';
    try {
      await api('/cloudflare/cancel', { method: 'POST', body: {} });
      await loadCloudflare();
    } catch (err) {
      toast(cfError(err), 'error');
    } finally {
      cfBusy = '';
    }
  }

  async function cfProvision(): Promise<void> {
    const hostname = cfProvisionHost.trim();
    if (!hostname) {
      toast(t('admin.settings.cfHostRequired'), 'error');
      return;
    }
    cfBusy = 'provision';
    try {
      await api('/cloudflare/provision', { method: 'POST', body: { hostname, tunnelName: cfTunnelName.trim() } });
      toast(t('admin.common.success'), 'success');
      await loadCloudflare();
    } catch (err) {
      toast(cfError(err), 'error');
    } finally {
      cfBusy = '';
    }
  }

  async function cfDisable(): Promise<void> {
    cfBusy = 'disable';
    try {
      await api('/cloudflare/disable', { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
      await loadCloudflare();
    } catch (err) {
      toast(cfError(err), 'error');
    } finally {
      cfBusy = '';
    }
  }

  async function cfDisconnect(): Promise<void> {
    const ok = await confirmDialog({
      title: t('admin.settings.cfDisconnect'),
      message: t('admin.settings.cfDisconnectConfirm'),
      confirmLabel: t('admin.settings.cfDisconnect'),
      danger: true
    });
    if (!ok) return;
    cfBusy = 'disconnect';
    try {
      await api('/cloudflare/disconnect', { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
      await loadCloudflare();
    } catch (err) {
      toast(cfError(err), 'error');
    } finally {
      cfBusy = '';
    }
  }

  async function toggleCfEnabled(): Promise<void> {
    const next = !(cf?.cloudflare.enabled ?? false);
    try {
      await api('/settings', { method: 'PATCH', body: { settings: { cloudflare_enabled: next ? '1' : '0' } } });
      await loadCloudflare();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  function cfTunnelLabel(): string {
    return t('admin.settings.cfTunnel.' + (cf?.tunnel?.status || 'stopped'));
  }

  function cfTunnelDot(): string {
    switch (cf?.tunnel?.status) {
      case 'running':
        return 'bg-emerald-500';
      case 'error':
      case 'unavailable':
        return 'bg-rose-500';
      case 'unconfigured':
        return 'bg-amber-500';
      default:
        return 'bg-slate-400';
    }
  }

  function cfCopyLogin(): void {
    const url = cf?.cloudflare.loginUrl || '';
    if (url) copyText(url).then(() => toast(t('common.copied'), 'success'));
  }

  // Cloudflare/DNS errors carry the underlying cloudflared message in `details`;
  // prefer it over the generic translated text so problems are diagnosable.
  function cfError(err: unknown): string {
    if (err instanceof APIError && err.details && (err.code === 'cloudflare_error' || err.code === 'dns_error')) {
      return err.details;
    }
    return errorText(err);
  }
</script>

{#if loading}
  <div class="space-y-4">{#each Array(4) as _}<div class="skeleton h-40"></div>{/each}</div>
{:else}
  <div class="space-y-5">
    {#if error}
      <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
        <Icon name="error" size={16} /><span>{error}</span>
      </p>
    {/if}

    <!-- Branding -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="image" size={18} />{t('admin.settings.branding')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.brandingHint')}</p>
      <div class="mt-4 space-y-4">
        <div>
          <label class="label" for="s-appname">{t('admin.settings.appName')}</label>
          <input id="s-appname" class="input max-w-sm" bind:value={appName} disabled={saving === 'branding'} />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          {#each brandingItems as item}
            <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
              <p class="text-sm font-medium text-slate-800 dark:text-slate-200">{item.label}</p>
              <div class="mt-3 flex items-center gap-3">
                {#if previews[item.kind]}
                  <img src={previews[item.kind]} alt={item.label} class="h-12 w-12 rounded-lg border border-brand-300 object-contain p-1 dark:border-brand-500/50" />
                {:else if item.has}
                  <img src={mediaURL(item.url)} alt={item.label} class="h-12 w-12 rounded-lg border border-slate-200 object-contain p-1 dark:border-slate-700" />
                {:else}
                  <img src={item.fallback} alt={item.label} class="h-12 w-12 rounded-lg border border-dashed border-slate-300 object-contain p-1 dark:border-slate-700" />
                {/if}
                <div class="flex flex-wrap gap-2">
                  <label class="btn-secondary btn-sm cursor-pointer">
                    {#if uploadBusy === item.kind}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="upload" size={15} />{/if}
                    {item.kind === 'logo' ? t('admin.settings.uploadLogo') : t('admin.settings.uploadFavicon')}
                    <input type="file" class="hidden" accept={item.accept} on:change={(e) => selectBranding(item.kind, e)} disabled={uploadBusy === item.kind} />
                  </label>
                  {#if previews[item.kind]}
                    <button type="button" class="btn-ghost btn-sm" on:click={() => discardPreview(item.kind)}>
                      <Icon name="close" size={15} />{t('common.cancel')}
                    </button>
                  {:else if item.has}
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={() => removeBranding(item.kind)} disabled={uploadBusy === item.kind}>
                      <Icon name="trash" size={15} />{t('admin.settings.removeImage')}
                    </button>
                  {/if}
                </div>
              </div>
              {#if previews[item.kind]}
                <p class="mt-2 text-xs font-medium text-brand-600 dark:text-brand-400">{t('admin.settings.preview')}</p>
              {:else if !item.has}
                <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('admin.settings.defaultInUse')}</p>
              {/if}
            </div>
          {/each}
        </div>
        <div class="flex justify-end">
          <button type="button" class="btn-primary" on:click={saveBranding} disabled={saving === 'branding'}>
            {#if saving === 'branding'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('common.save')}
          </button>
        </div>
      </div>
    </section>

    <!-- General -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="settings" size={18} />{t('admin.settings.general')}</h2>
      <div class="mt-4 space-y-4">
        <div>
          <label class="label" for="s-baseurl">{t('admin.settings.publicBaseUrl')}</label>
          <input id="s-baseurl" class="input" placeholder="https://share.example.com" bind:value={publicBaseUrl} disabled={saving === 'general'} />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="label" for="s-defaultret">{t('admin.settings.defaultRetention')}</label>
            <input id="s-defaultret" class="input" type="number" min="0" bind:value={defaultShareRetentionDays} disabled={saving === 'general'} />
          </div>
          <div>
            <label class="label" for="s-monrefresh">{t('admin.settings.monitoringRefresh')}</label>
            <input id="s-monrefresh" class="input" type="number" min="1" max="60" bind:value={monitoringRefreshSeconds} disabled={saving === 'general'} />
          </div>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <Toggle bind:checked={forceHttpsLinks} label={t('admin.settings.forceHttps')} />
          <Toggle bind:checked={requireDomainVerification} label={t('admin.settings.requireDomainVerification')} />
          <Toggle bind:checked={allow2fa} label={t('admin.settings.allow2fa')} />
        </div>
        <div class="flex justify-end">
          <button type="button" class="btn-primary" on:click={saveGeneral} disabled={saving === 'general'}>
            {#if saving === 'general'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('common.save')}
          </button>
        </div>
      </div>
    </section>

    <!-- Storage -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="harddrive" size={18} />{t('admin.settings.storage')}</h2>
      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="s-defstore">{t('admin.settings.defaultStorage')} (GiB)</label>
          <input id="s-defstore" class="input" type="number" min="0" bind:value={defaultStorageGb} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-globstore">{t('admin.settings.globalStorage')} (GiB)</label>
          <input id="s-globstore" class="input" type="number" min="0" bind:value={globalStorageGb} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-maxfile">{t('admin.settings.maxFile')} (MiB)</label>
          <input id="s-maxfile" class="input" type="number" min="1" bind:value={maxFileMb} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-maxshare">{t('admin.settings.maxShare')} (MiB)</label>
          <input id="s-maxshare" class="input" type="number" min="1" bind:value={maxShareMb} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-resumable">{t('admin.settings.resumable')} (MiB)</label>
          <input id="s-resumable" class="input" type="number" min="1" bind:value={resumableMb} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-trashret">{t('admin.settings.trashRetention')}</label>
          <input id="s-trashret" class="input" type="number" min="0" bind:value={trashRetentionDays} disabled={saving === 'storage'} />
        </div>
        <div>
          <label class="label" for="s-statsret">{t('admin.settings.statsRetention')}</label>
          <input id="s-statsret" class="input" type="number" min="1" bind:value={statsRetentionDays} disabled={saving === 'storage'} />
        </div>
      </div>
      <div class="mt-4"><Toggle bind:checked={allowUnlimitedStorage} label={t('admin.settings.allowUnlimited')} /></div>
      <div class="mt-4 flex justify-end">
        <button type="button" class="btn-primary" on:click={saveStorage} disabled={saving === 'storage'}>
          {#if saving === 'storage'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </section>

    <!-- Security -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="shield" size={18} />{t('admin.settings.security')}</h2>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div>
          <label class="label" for="s-session">{t('admin.settings.sessionHours')}</label>
          <input id="s-session" class="input" type="number" min="1" bind:value={maxSessionHours} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-remember">{t('admin.settings.rememberDays')}</label>
          <input id="s-remember" class="input" type="number" min="1" bind:value={rememberMeDays} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-shareaccess">{t('admin.settings.shareAccess')}</label>
          <input id="s-shareaccess" class="input" type="number" min="1" bind:value={shareAccessHours} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-pwmin">{t('admin.settings.passwordMin')}</label>
          <input id="s-pwmin" class="input" type="number" min="8" bind:value={passwordMinLength} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-loginrate">{t('admin.settings.loginRate')}</label>
          <input id="s-loginrate" class="input" type="number" min="1" bind:value={loginRatePerMinute} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-lockfail">{t('admin.settings.lockFailures')}</label>
          <input id="s-lockfail" class="input" type="number" min="1" bind:value={loginFailuresBeforeLock} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-lockmin">{t('admin.settings.lockMinutes')}</label>
          <input id="s-lockmin" class="input" type="number" min="1" bind:value={lockoutMinutes} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-apirate">{t('admin.settings.apiRate')}</label>
          <input id="s-apirate" class="input" type="number" min="1" bind:value={apiRatePerMinute} disabled={saving === 'security'} />
        </div>
        <div>
          <label class="label" for="s-2fatry">{t('admin.settings.twofaAttempts')}</label>
          <input id="s-2fatry" class="input" type="number" min="1" bind:value={twofaAttempts} disabled={saving === 'security'} />
        </div>
      </div>
      <div class="mt-4 flex justify-end">
        <button type="button" class="btn-primary" on:click={saveSecurity} disabled={saving === 'security'}>
          {#if saving === 'security'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </section>

    <!-- Uploads -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="upload" size={18} />{t('admin.settings.uploads')}</h2>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div>
          <label class="label" for="s-upconc">{t('admin.settings.uploadConcurrency')}</label>
          <input id="s-upconc" class="input" type="number" min="1" bind:value={uploadConcurrencyPerUser} disabled={saving === 'uploads'} />
        </div>
        <div>
          <label class="label" for="s-downconc">{t('admin.settings.downloadConcurrency')}</label>
          <input id="s-downconc" class="input" type="number" min="1" bind:value={downloadConcurrencyPerUser} disabled={saving === 'uploads'} />
        </div>
        <div>
          <label class="label" for="s-globalup">{t('admin.settings.globalUpload')}</label>
          <input id="s-globalup" class="input" type="number" min="1" bind:value={globalUploadLimit} disabled={saving === 'uploads'} />
        </div>
        <div>
          <label class="label" for="s-globaldown">{t('admin.settings.globalDownload')}</label>
          <input id="s-globaldown" class="input" type="number" min="1" bind:value={globalDownloadLimit} disabled={saving === 'uploads'} />
        </div>
        <div>
          <label class="label" for="s-zipconc">{t('admin.settings.zipConcurrency')}</label>
          <input id="s-zipconc" class="input" type="number" min="1" bind:value={zipConcurrency} disabled={saving === 'uploads'} />
        </div>
        <div>
          <label class="label" for="s-queue">{t('admin.settings.uploadQueue')}</label>
          <input id="s-queue" class="input" type="number" min="1" bind:value={uploadQueueLimit} disabled={saving === 'uploads'} />
        </div>
      </div>
      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="s-blockext">{t('admin.settings.blockedExtensions')}</label>
          <textarea id="s-blockext" class="input min-h-[5rem] font-mono text-xs" placeholder="exe, bat, cmd" bind:value={blockedExtensions} disabled={saving === 'uploads'}></textarea>
        </div>
        <div>
          <label class="label" for="s-blockmime">{t('admin.settings.blockedMime')}</label>
          <textarea id="s-blockmime" class="input min-h-[5rem] font-mono text-xs" placeholder="application/x-msdownload" bind:value={blockedMime} disabled={saving === 'uploads'}></textarea>
        </div>
      </div>
      <div class="mt-4 flex justify-end">
        <button type="button" class="btn-primary" on:click={saveUploads} disabled={saving === 'uploads'}>
          {#if saving === 'uploads'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </section>

    <!-- Antivirus -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="shield" size={18} />{t('admin.settings.antivirus')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.antivirusHint')}</p>
      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <Toggle bind:checked={antivirusEnabled} label={t('admin.settings.antivirusEnabled')} />
        <div>
          <label class="label" for="s-freshclam">{t('admin.settings.freshclamHours')}</label>
          <input id="s-freshclam" class="input" type="number" min="1" max="168" bind:value={freshclamIntervalHours} disabled={saving === 'antivirus'} />
        </div>
      </div>
      <div class="mt-4 flex justify-end">
        <button type="button" class="btn-primary" on:click={saveAntivirus} disabled={saving === 'antivirus'}>
          {#if saving === 'antivirus'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </section>

    <!-- Cloudflare -->
    <section class="card card-pad">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="cloud" size={18} />{t('admin.settings.cloudflare')}</h2>
        {#if cf}
          {#if cf.cloudflare.enabled && cf.tunnel?.status === 'running'}
            <span class="badge-green">{t('admin.settings.cfState.running')}</span>
          {:else if cf.cloudflare.connected}
            <span class="badge-amber">{t('admin.settings.cfState.connected')}</span>
          {:else}
            <span class="badge-slate">{t('admin.settings.cfState.disconnected')}</span>
          {/if}
        {/if}
      </div>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.cloudflareHint')}</p>

      {#if cf}
        {#if !cf.cloudflare.available}
          <p class="mt-4 flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-500/10 dark:text-amber-300">
            <Icon name="warning" size={16} />
            <span>{t('admin.settings.cfUnavailable')}</span>
          </p>
        {/if}

        {#if !cf.cloudflare.connected}
          <!-- Step 1: browser login -->
          <div class="mt-4 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
            {#if cf.cloudflare.loginState === 'waiting'}
              <p class="text-sm font-medium text-slate-800 dark:text-slate-200">{t('admin.settings.cfLoginWaiting')}</p>
              {#if cf.cloudflare.loginUrl}
                <div class="mt-3 flex flex-wrap items-center gap-2">
                  <a class="btn-primary btn-sm" href={cf.cloudflare.loginUrl} target="_blank" rel="noreferrer"><Icon name="external" size={15} />{t('admin.settings.cfOpenDashboard')}</a>
                  <button type="button" class="btn-secondary btn-sm" on:click={cfCopyLogin}><Icon name="copy" size={15} />{t('common.copy')}</button>
                  <button type="button" class="btn-ghost btn-sm" on:click={cfCancelLogin} disabled={cfBusy === 'cancel'}><Icon name="close" size={15} />{t('common.cancel')}</button>
                </div>
                <p class="mt-2 break-all font-mono text-xs text-slate-500 dark:text-slate-400">{cf.cloudflare.loginUrl}</p>
              {:else}
                <p class="mt-3 flex items-center gap-2 text-sm text-slate-500 dark:text-slate-400"><Icon name="spinner" size={15} class="animate-spin" />{t('admin.settings.cfLoginPreparing')}</p>
              {/if}
              {#if cf.cloudflare.loginError}
                <p class="mt-2 text-sm text-rose-600 dark:text-rose-400">{cf.cloudflare.loginError}</p>
              {/if}
            {:else}
              <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.cfConnectHint')}</p>
              <button type="button" class="btn-primary mt-3" on:click={cfLogin} disabled={cfBusy === 'login' || !cf.cloudflare.available}>
                {#if cfBusy === 'login'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="cloud" size={16} />{/if}{t('admin.settings.cfConnect')}
              </button>
              {#if cf.cloudflare.loginState === 'error' && cf.cloudflare.loginError}
                <p class="mt-2 text-sm text-rose-600 dark:text-rose-400">{cf.cloudflare.loginError}</p>
              {/if}
            {/if}
          </div>
        {:else}
          <!-- Step 2: tunnel, hostname, connector -->
          <div class="mt-4 space-y-4">
            <div class="grid gap-4 sm:grid-cols-2">
              <div>
                <label class="label" for="s-cftunnel">{t('admin.settings.cfTunnelName')}</label>
                <input id="s-cftunnel" class="input" bind:value={cfTunnelName} disabled={cfBusy === 'provision'} />
              </div>
              <div>
                <label class="label" for="s-cfhost">{t('admin.settings.cfProvisionHost')}</label>
                <input id="s-cfhost" class="input" placeholder="share.example.com" bind:value={cfProvisionHost} disabled={cfBusy === 'provision'} />
              </div>
            </div>

            <div on:click={() => toggleCfEnabled()}>
              <Toggle checked={cf.cloudflare.enabled} label={t('admin.settings.cfEnabled')} disabled={cfBusy === 'disable'} />
            </div>

            <div class="rounded-xl border border-slate-200 p-3 text-sm dark:border-slate-800">
              <div class="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span class="font-medium text-slate-700 dark:text-slate-300">{t('admin.settings.cfStatus')}:</span>
                <span class="inline-flex items-center gap-1.5 text-slate-700 dark:text-slate-300">
                  <span class="h-2 w-2 rounded-full {cfTunnelDot()}"></span>
                  {cfTunnelLabel()}
                </span>
                {#if cf.cloudflare.tunnelId}<span class="font-mono text-xs text-slate-400">{cf.cloudflare.tunnelId}</span>{/if}
                {#if cf.cloudflare.hostname}<span class="text-slate-500 dark:text-slate-400">→ {cf.cloudflare.hostname}</span>{/if}
              </div>
              {#if cf.tunnel?.message}<p class="mt-1 text-xs text-slate-500 dark:text-slate-400">{cf.tunnel.message}</p>{/if}
            </div>

            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn-primary btn-sm" on:click={cfProvision} disabled={cfBusy === 'provision' || !cf.cloudflare.available}>
                {#if cfBusy === 'provision'}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="network" size={15} />{/if}{t('admin.settings.cfProvision')}
              </button>
              <button type="button" class="btn-secondary btn-sm" on:click={cfDisable} disabled={cfBusy === 'disable' || !cf.cloudflare.enabled}><Icon name="ban" size={15} />{t('admin.settings.cfDisable')}</button>
              <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={cfDisconnect} disabled={cfBusy === 'disconnect'}><Icon name="trash" size={15} />{t('admin.settings.cfDisconnect')}</button>
            </div>
          </div>
        {/if}
      {/if}
    </section>

    <!-- Backups -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="backup" size={18} />{t('admin.settings.backups')}</h2>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div>
          <label class="label" for="s-bksched">{t('admin.settings.backupSchedule')}</label>
          <select id="s-bksched" class="input" bind:value={backupSchedule} disabled={saving === 'backups'}>
            <option value="off">{t('admin.backups.schedule.off')}</option>
            <option value="daily">{t('admin.backups.schedule.daily')}</option>
            <option value="weekly">{t('admin.backups.schedule.weekly')}</option>
          </select>
        </div>
        <div>
          <label class="label" for="s-bkhour">{t('admin.settings.backupHour')}</label>
          <input id="s-bkhour" class="input" type="number" min="0" max="23" bind:value={backupHour} disabled={saving === 'backups'} />
        </div>
        <div>
          <label class="label" for="s-bkret">{t('admin.settings.backupRetention')}</label>
          <input id="s-bkret" class="input" type="number" min="1" bind:value={backupRetention} disabled={saving === 'backups'} />
        </div>
      </div>
      <div class="mt-4">
        <label class="label" for="s-bkpath">{t('admin.settings.backupPath')}</label>
        <input id="s-bkpath" class="input" placeholder={backupDirectory} bind:value={backupPath} disabled={saving === 'backups'} />
      </div>
      <div class="mt-4"><Toggle bind:checked={backupEncryptAuto} label={t('admin.settings.backupEncrypt')} /></div>
      <div class="mt-4 flex justify-end">
        <button type="button" class="btn-primary" on:click={saveBackups} disabled={saving === 'backups'}>
          {#if saving === 'backups'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </section>

    <!-- Maintenance / danger zone -->
    <section class="card card-pad border-amber-200 dark:border-amber-500/40">
      <h2 class="flex items-center gap-2 text-base font-semibold text-amber-700 dark:text-amber-300"><Icon name="warning" size={18} />{t('admin.settings.maintenance')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.maintenanceHint')}</p>
      <div class="mt-4">
        <div on:click={() => setMaintenance(!maintenanceMode)}>
          <Toggle checked={maintenanceMode} label={t('admin.settings.maintenance')} />
        </div>
      </div>
    </section>

    {#if system}
      <p class="text-center text-xs text-slate-400 dark:text-slate-500">{system.go} · {system.os}/{system.arch} · v{system.version}</p>
    {/if}
  </div>
{/if}
