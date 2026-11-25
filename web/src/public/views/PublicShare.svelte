<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, downloadHref, errorText, fetchBlob, mediaURL, saveBlob } from '../../lib/api';
  import {
    decryptContainerStream,
    decryptContainerToBlob,
    decryptManifest,
    masterKeyFromInput,
    parseKeyFragment,
    unwrapWithPassword,
    unwrapWithRecovery,
    type E2EEManifestData
  } from '../../lib/e2ee';
  import { formatBytes, formatDate, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { publicMeta, toast } from '../../lib/stores';
  import { getOwnerShare } from '../../lib/tokens';
  import type { PublicSharePayload, ShareFile } from '../../lib/types';
  import { StreamingZip } from '../../lib/zip';
  import Footer from '../../components/Footer.svelte';
  import Icon from '../../components/Icon.svelte';
  import LocaleTheme from '../../components/LocaleTheme.svelte';
  import Modal from '../../components/Modal.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';

  export let token = '';

  let loading = true;
  let error = '';
  let payload: PublicSharePayload | null = null;

  let password = '';
  let unlocking = false;
  let unlockError = '';

  let e2eeKey: Uint8Array | null = null;
  let e2eeInput = '';
  let e2eeMode: 'password' | 'recovery' = 'password';
  let decrypting = false;
  let decryptError = '';

  function onE2eeInput(event: Event): void {
    e2eeInput = (event.currentTarget as HTMLInputElement).value;
  }
  let manifest: E2EEManifestData | null = null;
  let names: Record<string, string> = {};

  let viewCounted = false;
  let downloading = '';
  let zipBusy = false;
  let zipProgress = 0;

  let previewFile: ShareFile | null = null;
  let previewUrl = '';
  let previewBusy = false;
  let previewError = '';

  $: brand = $publicMeta?.appName || t('app.name');
  $: logo = mediaURL($publicMeta?.branding?.logo ? $publicMeta.branding.logoUrl : '');
  $: available = payload?.available === true;
  $: needsPassword = Boolean(payload?.requiresPassword && !payload?.accessGranted);
  $: isE2ee = Boolean(payload?.isE2ee);
  $: downloadAllowed = payload?.downloadAllowed === true;
  $: files = payload?.files || [];
  $: hasKey = e2eeKey !== null;
  $: e2eeLocked = isE2ee && available && !needsPassword && !hasKey && !decrypting;
  $: showContent = available && !needsPassword && (!isE2ee || hasKey);
  $: displayTitle = isE2ee ? manifest?.title || '' : payload?.title || '';
  $: displayDescription = isE2ee ? manifest?.description || '' : payload?.description || '';
  $: displayText = isE2ee ? manifest?.textContent || '' : payload?.textContent || '';
  $: displayLink = isE2ee ? manifest?.linkUrl || '' : payload?.linkUrl || '';
  $: shareType = isE2ee ? manifest?.type || payload?.type || '' : payload?.type || '';
  $: downloadsRemaining = payload?.downloadsRemaining;

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      const res = await api<PublicSharePayload>(`/share/${encodeURIComponent(token)}`, { auth: false });
      apply(res);
    } catch (err) {
      error = errorText(err);
    } finally {
      loading = false;
    }
  }

  function apply(res: PublicSharePayload): void {
    payload = res;
    if (res.isE2ee && res.e2eeManifest) {
      let key = parseKeyFragment(window.location.hash);
      if (!key) {
        const record = getOwnerShare(res.id);
        if (record.key) key = masterKeyFromInput(record.key);
      }
      if (key) {
        e2eeKey = key;
        void tryDecrypt();
      }
    }
    if (res.available && res.accessGranted && !res.isE2ee) void countView();
  }

  async function tryDecrypt(): Promise<void> {
    if (!payload || !e2eeKey || !payload.e2eeManifest) return;
    decrypting = true;
    decryptError = '';
    try {
      const data = await decryptManifest(payload.e2eeManifest, e2eeKey);
      manifest = data;
      names = {};
      const serverFiles = payload.files || [];
      data.files.forEach((file, index) => {
        const serverFile = serverFiles[index];
        if (serverFile) names[serverFile.id] = file.name;
      });
      void countView();
    } catch {
      e2eeKey = null;
      manifest = null;
      decryptError = t('share.decryptFailed');
    } finally {
      decrypting = false;
    }
  }

  async function unlock(event: Event): Promise<void> {
    event.preventDefault();
    if (!password) return;
    unlocking = true;
    unlockError = '';
    try {
      const res = await api<PublicSharePayload>(`/share/${encodeURIComponent(token)}/unlock`, {
        method: 'POST',
        auth: false,
        body: { password }
      });
      password = '';
      apply(res);
    } catch (err) {
      unlockError = errorText(err);
    } finally {
      unlocking = false;
    }
  }

  async function submitE2EE(event: Event): Promise<void> {
    event.preventDefault();
    if (!payload || !e2eeInput.trim()) return;
    decrypting = true;
    decryptError = '';
    try {
      const key =
        e2eeMode === 'password'
          ? await unwrapWithPassword(payload.e2eeKeyWrap || '', e2eeInput)
          : await unwrapWithRecovery(payload.e2eeRecoveryWrap || '', e2eeInput);
      e2eeKey = key;
      e2eeInput = '';
      await tryDecrypt();
    } catch {
      decryptError = t('share.invalidKey');
    } finally {
      decrypting = false;
    }
  }

  async function countView(): Promise<void> {
    if (viewCounted) return;
    viewCounted = true;
    try {
      await api(`/share/${encodeURIComponent(token)}/view`, { method: 'POST', auth: false, body: {} });
    } catch {
      // View counting is best effort.
    }
  }

  function fileName(file: ShareFile): string {
    if (isE2ee) return names[file.id] || t('detail.encryptedFile');
    return file.name || file.id;
  }

  function manifestEntry(file: ShareFile) {
    if (!manifest) return null;
    return manifest.files[files.indexOf(file)] || null;
  }

  function filePlainSize(file: ShareFile): number {
    const entry = manifestEntry(file);
    return entry ? entry.size : file.sizeBytes;
  }

  function fileMime(file: ShareFile): string {
    const entry = manifestEntry(file);
    return entry ? entry.mime : file.mime;
  }

  function previewKind(file: ShareFile): string {
    if (isE2ee) {
      const mime = fileMime(file);
      if (mime.startsWith('image/')) return 'image';
      if (mime.startsWith('video/')) return 'video';
      if (mime.startsWith('audio/')) return 'audio';
      if (mime === 'application/pdf') return 'pdf';
      return '';
    }
    return file.previewKind || '';
  }

  function fileIcon(file: ShareFile): string {
    switch (previewKind(file)) {
      case 'image':
        return 'image';
      case 'video':
        return 'video';
      case 'audio':
        return 'music';
      default:
        return shareType === 'folder' ? 'archive' : 'file';
    }
  }

  function plainUrl(file: ShareFile, inline = false): string {
    return downloadHref(`/share/${encodeURIComponent(token)}/files/${encodeURIComponent(file.id)}/${inline ? 'inline' : 'download'}`);
  }

  async function fetchCipher(file: ShareFile): Promise<Blob> {
    return fetchBlob(`/share/${encodeURIComponent(token)}/e2ee/${encodeURIComponent(file.id)}`, undefined, {
      headers: { Range: 'bytes=0-' }
    });
  }

  async function startTicket(): Promise<string> {
    const res = await api<{ ticket: string }>(`/share/${encodeURIComponent(token)}/e2ee-zip/start`, {
      method: 'POST',
      auth: false,
      body: {}
    });
    return res.ticket;
  }

  async function completeTicket(ticket: string, bytes: number, cancel: boolean): Promise<void> {
    await api(`/share/${encodeURIComponent(token)}/e2ee-zip/complete`, {
      method: 'POST',
      auth: false,
      body: { ticket, bytes, cancel }
    });
  }

  function bumpDownload(bytes: number): void {
    if (!payload) return;
    payload = {
      ...payload,
      downloadCount: (payload.downloadCount || 0) + 1,
      downloadsRemaining: typeof payload.downloadsRemaining === 'number' ? Math.max(0, payload.downloadsRemaining - 1) : payload.downloadsRemaining
    };
    void bytes;
  }

  async function downloadFile(file: ShareFile): Promise<void> {
    if (!downloadAllowed) {
      toast(t('share.downloadLimitReached'), 'error');
      return;
    }
    if (!isE2ee) {
      window.location.href = plainUrl(file);
      return;
    }
    if (!e2eeKey) return;
    downloading = file.id;
    try {
      const ticket = await startTicket();
      try {
        const blob = await fetchCipher(file);
        const plain = await decryptContainerToBlob(blob, e2eeKey);
        saveBlob(plain, fileName(file));
        await completeTicket(ticket, plain.size, false);
        bumpDownload(plain.size);
      } catch (err) {
        await completeTicket(ticket, 0, true).catch(() => undefined);
        throw err;
      }
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      downloading = '';
    }
  }

  async function downloadZip(): Promise<void> {
    if (!downloadAllowed) {
      toast(t('share.downloadLimitReached'), 'error');
      return;
    }
    if (!isE2ee) {
      window.location.href = downloadHref(`/share/${encodeURIComponent(token)}/zip`);
      return;
    }
    if (!e2eeKey || !manifest) return;
    zipBusy = true;
    zipProgress = 0;
    try {
      const ticket = await startTicket();
      try {
        const zip = new StreamingZip();
        let done = 0;
        for (const file of files) {
          const blob = await fetchCipher(file);
          await zip.add(fileName(file), decryptContainerStream(blob, e2eeKey));
          done += 1;
          zipProgress = files.length ? Math.round((done / files.length) * 100) : 100;
        }
        const out = await zip.end();
        saveBlob(out, `${payload?.slug || 'share'}.zip`);
        await completeTicket(ticket, out.size, false);
        bumpDownload(out.size);
      } catch (err) {
        await completeTicket(ticket, 0, true).catch(() => undefined);
        throw err;
      }
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      zipBusy = false;
      zipProgress = 0;
    }
  }

  async function openPreview(file: ShareFile): Promise<void> {
    previewError = '';
    previewBusy = true;
    previewFile = file;
    try {
      if (isE2ee) {
        if (!e2eeKey) return;
        const blob = await fetchCipher(file);
        const plain = await decryptContainerToBlob(blob, e2eeKey);
        previewUrl = URL.createObjectURL(plain);
      } else {
        previewUrl = plainUrl(file, true);
      }
    } catch (err) {
      previewError = errorText(err);
    } finally {
      previewBusy = false;
    }
  }

  function closePreview(): void {
    if (isE2ee && previewUrl.startsWith('blob:')) URL.revokeObjectURL(previewUrl);
    previewFile = null;
    previewUrl = '';
    previewError = '';
  }

  async function copyTextContent(): Promise<void> {
    try {
      await copyText(displayText);
      toast(t('share.textCopied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  function unavailableMessage(): string {
    switch (payload?.state) {
      case 'expired':
        return t('share.expired');
      case 'deleted':
        return t('err.share_in_trash');
      case 'locked':
        return t('err.admin_locked');
      default:
        return t('share.unavailable');
    }
  }
</script>

<div class="flex min-h-screen flex-col bg-slate-50 dark:bg-slate-950">
  <header class="border-b border-slate-200 bg-white/85 backdrop-blur dark:border-slate-800 dark:bg-slate-950/85">
    <div class="mx-auto flex h-16 max-w-3xl items-center justify-between gap-3 px-4">
      <a href="/" class="flex min-w-0 items-center gap-2.5">
        {#if logo}
          <img src={logo} alt="" class="h-8 w-auto max-w-[9rem] object-contain" />
        {:else}
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-brand-600 text-white">
            <Icon name="shield" size={18} />
          </span>
          <span class="truncate text-base font-bold tracking-tight text-slate-900 dark:text-white">{brand}</span>
        {/if}
      </a>
      <LocaleTheme compact />
    </div>
  </header>

  <main class="mx-auto w-full max-w-3xl flex-1 px-4 py-8">
    {#if loading}
      <div class="card card-pad space-y-4">
        <div class="skeleton h-7 w-1/2"></div>
        <div class="skeleton h-4 w-3/4"></div>
        <div class="skeleton h-24 w-full"></div>
      </div>
    {:else if error}
      <div class="card card-pad text-center">
        <span class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-rose-100 text-rose-600 dark:bg-rose-500/15 dark:text-rose-300">
          <Icon name="error" size={26} />
        </span>
        <h1 class="mt-4 text-lg font-semibold text-slate-900 dark:text-white">{t('share.notFound')}</h1>
        <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{error}</p>
      </div>
    {:else if payload && !available}
      <div class="card card-pad text-center">
        <span class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300">
          <Icon name="warning" size={26} />
        </span>
        <h1 class="mt-4 text-lg font-semibold text-slate-900 dark:text-white">{unavailableMessage()}</h1>
        {#if payload.expiresAt}
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('share.expiryBadge', { date: formatDate(payload.expiresAt) })}</p>
        {/if}
      </div>
    {:else if payload}
      <div class="space-y-5">
        <div class="flex flex-wrap items-center gap-2">
          {#if isE2ee}<span class="badge-brand"><Icon name="lock" size={12} />{t('share.encryptedBadge')}</span>{/if}
          {#if payload.requiresPassword}<span class="badge-amber"><Icon name="key" size={12} />{t('share.passwordBadge')}</span>{/if}
          {#if payload.expiresAt}<span class="badge-slate"><Icon name="clock" size={12} />{t('share.expiryBadge', { date: formatDate(payload.expiresAt) })}</span>{/if}
          {#if downloadsRemaining !== undefined}
            <span class="badge-slate"><Icon name="download" size={12} />{t('share.downloadsRemaining', { count: formatNumber(downloadsRemaining) })}</span>
          {:else}
            <span class="badge-slate"><Icon name="download" size={12} />{t('share.downloadsUnlimited')}</span>
          {/if}
        </div>

        {#if needsPassword}
          <section class="card card-pad">
            <span class="flex h-11 w-11 items-center justify-center rounded-2xl bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300">
              <Icon name="lock" size={22} />
            </span>
            <h1 class="mt-3 text-xl font-bold text-slate-900 dark:text-white">{t('share.passwordTitle')}</h1>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('share.passwordHint')}</p>
            {#if displayTitle}
              <p class="mt-3 text-sm font-medium text-slate-700 dark:text-slate-200">{displayTitle}</p>
            {/if}
            <form class="mt-5 space-y-3" on:submit={unlock}>
              <div>
                <label class="label" for="share-password">{t('auth.password')}</label>
                <input id="share-password" class="input" type="password" autocomplete="off" bind:value={password} disabled={unlocking} autofocus />
              </div>
              {#if unlockError}
                <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
                  <Icon name="error" size={16} /><span>{unlockError}</span>
                </p>
              {/if}
              <button type="submit" class="btn-primary w-full" disabled={unlocking || !password}>
                {#if unlocking}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="unlock" size={16} />{/if}
                {t('share.unlock')}
              </button>
            </form>
          </section>
        {:else if e2eeLocked}
          <section class="card card-pad">
            <span class="flex h-11 w-11 items-center justify-center rounded-2xl bg-brand-100 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300">
              <Icon name="lock" size={22} />
            </span>
            <h1 class="mt-3 text-xl font-bold text-slate-900 dark:text-white">{t('share.e2eeTitle')}</h1>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
              {payload.e2eeMode === 'password' ? t('share.e2eePasswordHint') : t('share.e2eeKeyHint')}
            </p>
            <form class="mt-5 space-y-3" on:submit={submitE2EE}>
              <div>
                <label class="label" for="e2ee-secret">
                  {e2eeMode === 'password' ? t('share.passwordLabel') : t('share.recoveryLabel')}
                </label>
                <input
                  id="e2ee-secret"
                  class="input"
                  type={e2eeMode === 'password' ? 'password' : 'text'}
                  autocomplete="off"
                  spellcheck="false"
                  value={e2eeInput}
                  on:input={onE2eeInput}
                  disabled={decrypting}
                />
              </div>
              {#if decryptError}
                <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
                  <Icon name="error" size={16} /><span>{decryptError}</span>
                </p>
              {/if}
              <button type="submit" class="btn-primary w-full" disabled={decrypting || !e2eeInput.trim()}>
                {#if decrypting}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="key" size={16} />{/if}
                {decrypting ? t('share.decrypting') : t('share.decrypt')}
              </button>
              {#if payload.e2eeRecoveryWrap}
                <div class="text-center text-sm">
                  <button type="button" class="link" on:click={() => (e2eeMode = e2eeMode === 'password' ? 'recovery' : 'password')}>
                    {e2eeMode === 'password' ? t('share.recoveryHint') : t('share.e2eePasswordHint')}
                  </button>
                </div>
              {/if}
            </form>
          </section>
        {:else if decrypting}
          <section class="card card-pad">
            <ProgressBar indeterminate label={t('share.decrypting')} />
          </section>
        {:else if showContent}
          <section class="card card-pad">
            <h1 class="text-xl font-bold tracking-tight text-slate-900 dark:text-white">{displayTitle || t('share.title')}</h1>
            {#if displayDescription}
              <p class="mt-2 whitespace-pre-line text-sm text-slate-600 dark:text-slate-300">{displayDescription}</p>
            {/if}
            {#if isE2ee}
              <p class="mt-3 flex items-start gap-2 rounded-xl bg-brand-50 px-3 py-2 text-xs text-brand-800 dark:bg-brand-500/10 dark:text-brand-200">
                <Icon name="lock" size={14} class="mt-0.5" />{t('share.viewOnceHint')}
              </p>
            {/if}
          </section>

          {#if shareType === 'link' && displayLink}
            <section class="card card-pad">
              <a class="btn-primary" href={displayLink} target="_blank" rel="noopener noreferrer nofollow">
                <Icon name="external" size={16} />{t('share.openLink')}
              </a>
              <p class="mt-2 truncate text-xs text-slate-500 dark:text-slate-400">{displayLink}</p>
            </section>
          {/if}

          {#if shareType === 'text'}
            <section class="card card-pad">
              <pre class="max-h-[28rem] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-slate-50 p-4 font-mono text-sm text-slate-800 dark:bg-slate-900 dark:text-slate-100">{displayText}</pre>
              <div class="mt-3">
                <button type="button" class="btn-secondary btn-sm" on:click={copyTextContent}><Icon name="copy" size={15} />{t('share.copyText')}</button>
              </div>
            </section>
          {/if}

          {#if files.length > 0}
            <section class="card card-pad">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white">
                  <Icon name={shareType === 'folder' ? 'folder' : 'file'} size={18} />{t('share.filesCount', { count: formatNumber(files.length) })}
                </h2>
                {#if files.length > 1}
                  <button type="button" class="btn-primary btn-sm" disabled={zipBusy || !downloadAllowed} on:click={downloadZip}>
                    {#if zipBusy}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="download" size={15} />{/if}
                    {t('share.downloadAll')}
                  </button>
                {/if}
              </div>

              {#if zipBusy}
                <div class="mt-3"><ProgressBar value={zipProgress} max={100} label={t('share.zipPreparing')} /></div>
              {/if}

              {#if !downloadAllowed}
                <p class="mt-3 flex items-center gap-2 rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">
                  <Icon name="warning" size={14} />{t('share.downloadLimitReached')}
                </p>
              {/if}

              <ul class="mt-4 divide-y divide-slate-100 dark:divide-slate-800">
                {#each files as file (file.id)}
                  <li class="flex items-center gap-3 py-3">
                    <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                      <Icon name={fileIcon(file)} size={18} />
                    </span>
                    <span class="min-w-0 flex-1">
                      <span class="block truncate text-sm font-medium text-slate-800 dark:text-slate-100">{fileName(file)}</span>
                      <span class="block text-xs text-slate-500 dark:text-slate-400">
                        {formatBytes(isE2ee ? filePlainSize(file) : file.sizeBytes)}
                        {#if file.scanStatus === 'infected'}· <span class="text-rose-600 dark:text-rose-400">{t('shares.quarantined')}</span>{/if}
                        {#if file.scanStatus === 'pending'}· <span class="text-amber-600 dark:text-amber-400">{t('admin.dashboard.pendingScans')}</span>{/if}
                      </span>
                    </span>
                    <div class="flex shrink-0 items-center gap-1">
                      {#if previewKind(file)}
                        <button type="button" class="btn-ghost btn-sm" title={t('common.preview')} on:click={() => openPreview(file)}>
                          <Icon name="eye" size={15} />
                        </button>
                      {/if}
                      <button
                        type="button"
                        class="btn-secondary btn-sm"
                        disabled={!downloadAllowed || downloading === file.id}
                        on:click={() => downloadFile(file)}
                      >
                        {#if downloading === file.id}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="download" size={15} />{/if}
                        <span class="hidden sm:inline">{t('share.downloadFile')}</span>
                      </button>
                    </div>
                  </li>
                {/each}
              </ul>
            </section>
          {/if}

          {#if payload.quarantine}
            <section class="card card-pad border-amber-200 dark:border-amber-500/30">
              <p class="flex items-center gap-2 text-sm text-amber-700 dark:text-amber-300">
                <Icon name="warning" size={16} />{t('admin.shares.quarantineHint')}
              </p>
            </section>
          {/if}
        {/if}
      </div>
    {/if}
  </main>

  <Footer version={$publicMeta?.version || ''} />
</div>

<Modal open={Boolean(previewFile)} title={previewFile ? fileName(previewFile) : ''} size="lg" on:close={closePreview}>
  {#if previewBusy}
    <div class="py-10"><ProgressBar indeterminate label={t('common.loading')} /></div>
  {:else if previewError}
    <p class="flex items-center gap-2 text-sm text-rose-600 dark:text-rose-400"><Icon name="error" size={18} />{previewError}</p>
  {:else if previewFile && previewUrl}
    {#if previewKind(previewFile) === 'image'}
      <img src={previewUrl} alt={fileName(previewFile)} class="mx-auto max-h-[70vh] rounded-xl object-contain" />
    {:else if previewKind(previewFile) === 'video'}
      <video src={previewUrl} controls class="mx-auto max-h-[70vh] w-full rounded-xl"></video>
    {:else if previewKind(previewFile) === 'audio'}
      <audio src={previewUrl} controls class="w-full"></audio>
    {:else}
      <iframe src={previewUrl} title={fileName(previewFile)} class="h-[70vh] w-full rounded-xl border border-slate-200 dark:border-slate-800"></iframe>
    {/if}
  {/if}
</Modal>
