<script lang="ts">
  import { api, errorText, setCSRF } from '../../lib/api';
  import { t } from '../../lib/i18n';
  import { mustChangePassword, setupCompleted, user } from '../../lib/stores';
  import type { User } from '../../lib/types';
  import Footer from '../../components/Footer.svelte';
  import Icon from '../../components/Icon.svelte';
  import LocaleTheme from '../../components/LocaleTheme.svelte';
  import { adminMeta, loadAdminShell } from '../meta';

  let username = '';
  let displayName = '';
  let password = '';
  let confirmPassword = '';
  let appName = '';
  let domain = '';
  let busy = false;
  let error = '';

  $: mismatch = confirmPassword.length > 0 && confirmPassword !== password;
  $: canSubmit =
    username.trim().length >= 3 && password.length >= 8 && password === confirmPassword && !busy;

  async function submit(event: Event): Promise<void> {
    event.preventDefault();
    error = '';
    if (!canSubmit) return;
    busy = true;
    try {
      const res = await api<{ ok: boolean; user: User; csrf: string }>('/setup', {
        method: 'POST',
        auth: false,
        body: {
          username: username.trim(),
          displayName: displayName.trim(),
          password,
          appName: appName.trim(),
          domain: domain.trim()
        }
      });
      setCSRF(res.csrf);
      user.set(res.user);
      mustChangePassword.set(Boolean(res.user.mustChangePassword));
      setupCompleted.set(true);
      void loadAdminShell();
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-screen flex-col bg-slate-50 dark:bg-slate-950">
  <div class="flex flex-1 items-center justify-center px-4 py-10">
    <div class="w-full max-w-lg">
      <div class="mb-6 flex items-center justify-between">
        <span class="flex items-center gap-2.5">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-600 text-white">
            <Icon name="settings" size={19} />
          </span>
          <span class="text-lg font-bold tracking-tight text-slate-900 dark:text-white">{t('admin.setup.title')}</span>
        </span>
        <LocaleTheme compact />
      </div>

      <div class="card card-pad fade-in">
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.setup.subtitle')}</p>

        <form class="mt-6 space-y-4" on:submit={submit}>
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="setup-username">{t('admin.setup.username')}</label>
              <input id="setup-username" class="input" autocomplete="username" autofocus bind:value={username} disabled={busy} />
            </div>
            <div>
              <label class="label" for="setup-display">{t('admin.setup.displayName')}</label>
              <input id="setup-display" class="input" autocomplete="name" bind:value={displayName} disabled={busy} />
            </div>
          </div>

          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="setup-password">{t('admin.setup.password')}</label>
              <input id="setup-password" class="input" type="password" autocomplete="new-password" bind:value={password} disabled={busy} />
            </div>
            <div>
              <label class="label" for="setup-confirm">{t('admin.setup.passwordConfirm')}</label>
              <input id="setup-confirm" class="input" type="password" autocomplete="new-password" bind:value={confirmPassword} disabled={busy} />
            </div>
          </div>

          {#if mismatch}
            <p class="text-sm text-rose-600 dark:text-rose-400">{t('admin.setup.passwordMismatch')}</p>
          {:else if password.length > 0 && password.length < 8}
            <p class="text-sm text-rose-600 dark:text-rose-400">{t('admin.setup.minLength')}</p>
          {/if}

          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="setup-appname">{t('admin.setup.appName')}</label>
              <input id="setup-appname" class="input" placeholder={$adminMeta.appName || 'TEX Share'} bind:value={appName} disabled={busy} />
            </div>
            <div>
              <label class="label" for="setup-domain">{t('admin.setup.domain')}</label>
              <input id="setup-domain" class="input" placeholder="localhost" bind:value={domain} disabled={busy} />
              <p class="hint">{t('admin.setup.domainHint')}</p>
            </div>
          </div>

          {#if error}
            <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
              <Icon name="error" size={16} /><span>{error}</span>
            </p>
          {/if}

          <button type="submit" class="btn-primary w-full" disabled={!canSubmit}>
            {#if busy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('admin.setup.submit')}
          </button>
        </form>
      </div>
    </div>
  </div>
  <Footer admin version={$adminMeta.version} />
</div>
