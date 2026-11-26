<script lang="ts">
  import { errorText } from '../../lib/api';
  import { t } from '../../lib/i18n';
  import { login, verifySecondFactor } from '../../lib/stores';
  import Footer from '../../components/Footer.svelte';
  import Icon from '../../components/Icon.svelte';
  import LocaleTheme from '../../components/LocaleTheme.svelte';
  import TexShareLogo from '../../components/TexShareLogo.svelte';
  import { adminMeta } from '../meta';

  let username = '';
  let password = '';
  let remember = false;
  let code = '';
  let challenge = '';
  let useRecovery = false;
  let busy = false;
  let showPassword = false;
  let error = '';

  function onPasswordInput(event: Event): void {
    password = (event.currentTarget as HTMLInputElement).value;
  }

  async function submitCredentials(event: Event): Promise<void> {
    event.preventDefault();
    error = '';
    if (!username.trim() || !password) {
      error = t('common.requiredField');
      return;
    }
    busy = true;
    try {
      const res = await login(username.trim(), password, remember);
      if (res.totpRequired) {
        challenge = res.challenge || '';
        password = '';
        code = '';
      }
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  async function submitCode(event: Event): Promise<void> {
    event.preventDefault();
    error = '';
    if (!code.trim()) {
      error = t('common.requiredField');
      return;
    }
    busy = true;
    try {
      const res = await verifySecondFactor(challenge, code.trim(), useRecovery);
      if (!res.ok) error = t('err.invalid_code');
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  function back(): void {
    challenge = '';
    code = '';
    error = '';
  }
</script>

<div class="flex min-h-screen flex-col bg-slate-50 dark:bg-slate-950">
  <div class="flex flex-1 items-center justify-center px-4 py-10">
    <div class="w-full max-w-md">
      <div class="mb-6 flex items-center justify-between">
        <span class="flex items-center gap-2.5">
          <TexShareLogo src={$adminMeta.logoUrl} size={36} />
          <span class="text-lg font-bold tracking-tight text-slate-900 dark:text-white">{$adminMeta.appName}</span>
        </span>
        <LocaleTheme compact />
      </div>

      <div class="card card-pad fade-in">
        <p class="mb-1 text-xs font-semibold uppercase tracking-wide text-brand-600 dark:text-brand-400">{t('admin.title')}</p>
        {#if !challenge}
          <h1 class="text-xl font-bold text-slate-900 dark:text-white">{t('auth.loginTitle')}</h1>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.login.subtitle')}</p>

          <form class="mt-6 space-y-4" on:submit={submitCredentials}>
            <div>
              <label class="label" for="admin-username">{t('auth.username')}</label>
              <input id="admin-username" class="input" autocomplete="username" autofocus bind:value={username} disabled={busy} />
            </div>
            <div>
              <label class="label" for="admin-password">{t('auth.password')}</label>
              <div class="relative">
                <input
                  id="admin-password"
                  class="input pr-11"
                  type={showPassword ? 'text' : 'password'}
                  autocomplete="current-password"
                  value={password}
                  on:input={onPasswordInput}
                  disabled={busy}
                />
                <button
                  type="button"
                  class="absolute inset-y-0 right-0 flex w-11 items-center justify-center text-slate-400 transition hover:text-slate-600 dark:hover:text-slate-200"
                  aria-label={t('auth.password')}
                  on:click={() => (showPassword = !showPassword)}
                >
                  <Icon name={showPassword ? 'eyeOff' : 'eye'} size={18} />
                </button>
              </div>
            </div>

            <label class="flex cursor-pointer items-center gap-2 text-sm text-slate-600 dark:text-slate-300">
              <input type="checkbox" class="h-4 w-4 rounded border-slate-300 text-brand-600 focus:ring-brand-500 dark:border-slate-700 dark:bg-slate-900" bind:checked={remember} />
              {t('auth.remember')}
            </label>

            {#if error}
              <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
                <Icon name="error" size={16} /><span>{error}</span>
              </p>
            {/if}

            <button type="submit" class="btn-primary w-full" disabled={busy}>
              {#if busy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="logout" size={16} />{/if}
              {busy ? t('auth.loggingIn') : t('auth.loginButton')}
            </button>
          </form>
        {:else}
          <h1 class="text-xl font-bold text-slate-900 dark:text-white">{t('auth.twoFactorTitle')}</h1>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('auth.twoFactorSubtitle')}</p>

          <form class="mt-6 space-y-4" on:submit={submitCode}>
            <div>
              <label class="label" for="admin-code">{useRecovery ? t('auth.recoveryCode') : t('auth.code')}</label>
              <input
                id="admin-code"
                class="input tracking-widest"
                inputmode={useRecovery ? 'text' : 'numeric'}
                autocomplete="one-time-code"
                autofocus
                bind:value={code}
                disabled={busy}
              />
            </div>

            {#if error}
              <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
                <Icon name="error" size={16} /><span>{error}</span>
              </p>
            {/if}

            <button type="submit" class="btn-primary w-full" disabled={busy}>
              {#if busy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
              {t('auth.verify')}
            </button>
            <div class="flex items-center justify-between text-sm">
              <button type="button" class="link" on:click={() => (useRecovery = !useRecovery)}>
                {useRecovery ? t('auth.useTOTP') : t('auth.useRecovery')}
              </button>
              <button type="button" class="text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200" on:click={back}>
                {t('common.back')}
              </button>
            </div>
          </form>
        {/if}
      </div>
    </div>
  </div>
  <Footer admin version={$adminMeta.version} />
</div>
