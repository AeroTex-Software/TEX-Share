<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../lib/api';
  import { formatDateTime, timeAgo } from '../lib/format';
  import { t } from '../lib/i18n';
  import { confirmDialog, patchUser, toast } from '../lib/stores';
  import type { SessionInfo } from '../lib/types';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import Toggle from './Toggle.svelte';

  export let totpEnabled = false;
  export let allow2fa = true;

  let sessions: SessionInfo[] = [];
  let loadingSessions = true;

  // Password change
  let currentPassword = '';
  let newPassword = '';
  let confirmPassword = '';
  let changingPassword = false;

  // 2FA setup
  let setupOpen = false;
  let setupData: { secret: string; otpauthUrl: string; qrDataUrl: string } | null = null;
  let setupCode = '';
  let setupBusy = false;
  let setupError = '';

  // 2FA disable
  let disableOpen = false;
  let disablePassword = '';
  let disableBusy = false;

  // Recovery codes
  let codesOpen = false;
  let recoveryCodes: string[] = [];
  let codesPassword = '';
  let codesCode = '';
  let codesBusy = false;
  let codesError = '';

  onMount(loadSessions);

  async function loadSessions(): Promise<void> {
    loadingSessions = true;
    try {
      const res = await api<{ sessions: SessionInfo[] }>('/auth/sessions');
      sessions = res.sessions || [];
    } catch {
      sessions = [];
    } finally {
      loadingSessions = false;
    }
  }

  async function changePassword(): Promise<void> {
    if (newPassword.length < 8) {
      toast(t('auth.passwordTooShort'), 'error');
      return;
    }
    if (newPassword !== confirmPassword) {
      toast(t('auth.passwordMismatch'), 'error');
      return;
    }
    changingPassword = true;
    try {
      await api('/auth/change-password', { method: 'POST', body: { currentPassword, newPassword } });
      currentPassword = '';
      newPassword = '';
      confirmPassword = '';
      patchUser({ mustChangePassword: false });
      toast(t('auth.passwordChanged'), 'success');
      loadSessions();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      changingPassword = false;
    }
  }

  async function startSetup(): Promise<void> {
    setupError = '';
    setupCode = '';
    try {
      setupData = await api('/auth/2fa/setup', { method: 'POST', body: {} });
      setupOpen = true;
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function enable2FA(): Promise<void> {
    if (!setupData) return;
    setupBusy = true;
    setupError = '';
    try {
      const res = await api<{ recoveryCodes: string[] }>('/auth/2fa/enable', {
        method: 'POST',
        body: { secret: setupData.secret, code: setupCode.trim() }
      });
      setupOpen = false;
      setupData = null;
      totpEnabled = true;
      patchUser({ totpEnabled: true });
      recoveryCodes = res.recoveryCodes || [];
      codesOpen = true;
      toast(t('auth.2fa.enabled'), 'success');
    } catch (err) {
      setupError = errorText(err);
    } finally {
      setupBusy = false;
    }
  }

  async function disable2FA(): Promise<void> {
    disableBusy = true;
    try {
      await api('/auth/2fa/disable', { method: 'POST', body: { password: disablePassword } });
      disableOpen = false;
      disablePassword = '';
      totpEnabled = false;
      patchUser({ totpEnabled: false });
      toast(t('auth.2fa.disabled'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      disableBusy = false;
    }
  }

  async function regenerateCodes(): Promise<void> {
    codesBusy = true;
    codesError = '';
    try {
      const res = await api<{ recoveryCodes: string[] }>('/auth/recovery-codes', {
        method: 'POST',
        body: { password: codesPassword, code: codesCode.trim() }
      });
      recoveryCodes = res.recoveryCodes || [];
      codesPassword = '';
      codesCode = '';
      toast(t('auth.2fa.regenerate'), 'success');
    } catch (err) {
      codesError = errorText(err);
    } finally {
      codesBusy = false;
    }
  }

  async function revokeSession(session: SessionInfo): Promise<void> {
    const ok = await confirmDialog({ message: t('auth.session.revoke') + '?', danger: true });
    if (!ok) return;
    try {
      await api(`/auth/sessions/${session.id}`, { method: 'DELETE' });
      toast(t('auth.sessions.revoked'), 'success');
      loadSessions();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function revokeOthers(): Promise<void> {
    const ok = await confirmDialog({ message: t('auth.sessions.revokeOthers') + '?', danger: true });
    if (!ok) return;
    try {
      await api('/auth/sessions/revoke-others', { method: 'POST', body: {} });
      toast(t('auth.sessions.revoked'), 'success');
      loadSessions();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function copyCode(code: string): Promise<void> {
    try {
      await copyText(code);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  function copySetupSecret(): void {
    if (setupData) void copyCode(setupData.secret);
  }
</script>

<div class="space-y-6">
  <!-- Password -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
      <Icon name="lock" size={19} />{t('auth.changePassword')}
    </h2>
    <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('auth.passwordChanged')}</p>
    <div class="mt-4 grid gap-4 sm:grid-cols-2">
      <div class="sm:col-span-2">
        <label class="label" for="cur-pass">{t('auth.currentPassword')}</label>
        <input id="cur-pass" class="input" type="password" autocomplete="current-password" bind:value={currentPassword} />
      </div>
      <div>
        <label class="label" for="new-pass">{t('auth.newPassword')}</label>
        <input id="new-pass" class="input" type="password" autocomplete="new-password" bind:value={newPassword} />
      </div>
      <div>
        <label class="label" for="conf-pass">{t('auth.confirmPassword')}</label>
        <input id="conf-pass" class="input" type="password" autocomplete="new-password" bind:value={confirmPassword} />
      </div>
    </div>
    <div class="mt-4">
      <button type="button" class="btn-primary" disabled={changingPassword || !currentPassword || !newPassword} on:click={changePassword}>
        {#if changingPassword}<Icon name="spinner" size={16} class="animate-spin" />{/if}
        {t('auth.changePassword')}
      </button>
    </div>
  </section>

  <!-- 2FA -->
  <section class="card card-pad">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
          <Icon name="qr" size={19} />{t('auth.2fa')}
        </h2>
        <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
          {totpEnabled ? t('auth.2fa.enabled') : t('auth.2fa.disabled')}
        </p>
      </div>
      <span class={totpEnabled ? 'badge-green' : 'badge-slate'}>
        <Icon name={totpEnabled ? 'check' : 'close'} size={13} />
        {totpEnabled ? t('auth.2fa.enabled') : t('auth.2fa.disabled')}
      </span>
    </div>
    {#if allow2fa}
      <div class="mt-4 flex flex-wrap gap-2">
        {#if !totpEnabled}
          <button type="button" class="btn-primary" on:click={startSetup}>
            <Icon name="qr" size={16} />{t('auth.2fa.setup')}
          </button>
        {:else}
          <button type="button" class="btn-secondary" on:click={() => (codesOpen = true)}>
            <Icon name="key" size={16} />{t('auth.2fa.recoveryCodes')}
          </button>
          <button type="button" class="btn-danger" on:click={() => (disableOpen = true)}>
            <Icon name="ban" size={16} />{t('auth.2fa.disable')}
          </button>
        {/if}
      </div>
    {:else}
      <p class="mt-3 text-sm text-amber-600 dark:text-amber-400">{t('auth.2fa.disabled')}</p>
    {/if}
  </section>

  <!-- Sessions -->
  <section class="card card-pad">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
        <Icon name="monitor" size={19} />{t('auth.sessions')}
      </h2>
      <button type="button" class="btn-secondary btn-sm" on:click={revokeOthers}>
        <Icon name="logout" size={15} />{t('auth.sessions.revokeOthers')}
      </button>
    </div>
    <div class="mt-4 space-y-2">
      {#if loadingSessions}
        <div class="skeleton h-14 w-full"></div>
      {:else if sessions.length === 0}
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('common.none')}</p>
      {:else}
        {#each sessions as session (session.id)}
          <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 px-4 py-3 dark:border-slate-800">
            <div class="min-w-0">
              <p class="flex items-center gap-2 text-sm font-medium text-slate-800 dark:text-slate-200">
                {session.browser || t('common.unknown')}
                {#if session.current}<span class="badge-brand">{t('auth.session.current')}</span>{/if}
              </p>
              <p class="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                {session.os || ''} · {timeAgo(session.lastSeenAt)} · {formatDateTime(session.expiresAt)}
              </p>
            </div>
            {#if !session.current}
              <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={() => revokeSession(session)}>
                {t('auth.session.revoke')}
              </button>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  </section>
</div>

<Modal open={setupOpen} title={t('auth.2fa.setup')} subtitle={t('auth.2fa.scan')} on:close={() => (setupOpen = false)}>
  {#if setupData}
    <div class="flex flex-col items-center gap-4">
      <img src={setupData.qrDataUrl} alt="QR" class="h-56 w-56 rounded-xl border border-slate-200 bg-white p-2 dark:border-slate-800" />
      <div class="w-full">
        <p class="label">{t('auth.2fa.secret')}</p>
        <div class="flex items-center gap-2">
          <code class="min-w-0 flex-1 truncate rounded-lg bg-slate-100 px-3 py-2 text-xs dark:bg-slate-800">{setupData.secret}</code>
          <button type="button" class="btn-secondary btn-sm" on:click={copySetupSecret}>
            <Icon name="copy" size={15} />
          </button>
        </div>
      </div>
      <div class="w-full">
        <label class="label" for="setup-code">{t('auth.code')}</label>
        <input id="setup-code" class="input tracking-[0.3em]" inputmode="numeric" maxlength="6" autocomplete="one-time-code" bind:value={setupCode} />
      </div>
      {#if setupError}<p class="text-sm text-rose-600 dark:text-rose-400">{setupError}</p>{/if}
      <button type="button" class="btn-primary w-full" disabled={setupBusy || setupCode.trim().length < 6} on:click={enable2FA}>
        {#if setupBusy}<Icon name="spinner" size={16} class="animate-spin" />{/if}
        {t('auth.2fa.enable')}
      </button>
    </div>
  {/if}
</Modal>

<Modal open={disableOpen} title={t('auth.2fa.disable')} on:close={() => (disableOpen = false)}>
  <p class="text-sm text-slate-600 dark:text-slate-300">{t('auth.2fa.disableConfirm')}</p>
  <div class="mt-4">
    <label class="label" for="disable-pass">{t('auth.password')}</label>
    <input id="disable-pass" class="input" type="password" autocomplete="current-password" bind:value={disablePassword} />
  </div>
  <div class="mt-6 flex justify-end gap-2">
    <button type="button" class="btn-secondary" on:click={() => (disableOpen = false)}>{t('common.cancel')}</button>
    <button type="button" class="btn-danger" disabled={disableBusy || !disablePassword} on:click={disable2FA}>
      {t('auth.2fa.disable')}
    </button>
  </div>
</Modal>

<Modal open={codesOpen} title={t('auth.2fa.recoveryCodes')} subtitle={t('auth.2fa.recoveryHint')} on:close={() => (codesOpen = false)}>
  {#if recoveryCodes.length > 0}
    <div class="grid grid-cols-2 gap-2">
      {#each recoveryCodes as code}
        <button type="button" class="rounded-lg bg-slate-100 px-3 py-2 text-center font-mono text-sm text-slate-800 transition hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700" on:click={() => copyCode(code)}>
          {code}
        </button>
      {/each}
    </div>
    <div class="mt-4">
      <button type="button" class="btn-secondary btn-sm" on:click={() => copyCode(recoveryCodes.join('\n'))}>
        <Icon name="copy" size={15} />{t('common.copy')}
      </button>
    </div>
  {:else}
    <p class="text-sm text-slate-600 dark:text-slate-300">{t('auth.2fa.regenerateConfirm')}</p>
    <div class="mt-4 space-y-3">
      <div>
        <label class="label" for="rc-pass">{t('auth.password')}</label>
        <input id="rc-pass" class="input" type="password" autocomplete="current-password" bind:value={codesPassword} />
      </div>
      <div>
        <label class="label" for="rc-code">{t('auth.code')}</label>
        <input id="rc-code" class="input tracking-[0.3em]" inputmode="numeric" maxlength="6" autocomplete="one-time-code" bind:value={codesCode} />
      </div>
      {#if codesError}<p class="text-sm text-rose-600 dark:text-rose-400">{codesError}</p>{/if}
      <button type="button" class="btn-primary w-full" disabled={codesBusy || !codesPassword || codesCode.trim().length < 6} on:click={regenerateCodes}>
        {#if codesBusy}<Icon name="spinner" size={16} class="animate-spin" />{/if}
        {t('auth.2fa.regenerate')}
      </button>
    </div>
  {/if}
</Modal>
