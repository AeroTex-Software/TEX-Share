<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, downloadHref, errorText } from '../../lib/api';
  import {
    createE2EEShareCrypto,
    decryptContainerToBlob,
    decryptManifest,
    encryptManifest,
    keyFragment,
    masterKeyFromInput,
    unwrapWithPassword,
    unwrapWithRecovery
  } from '../../lib/e2ee';
  import { formatBytes, formatDateTime, formatNumber, fromDatetimeLocal, toDatetimeLocal } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { ownerShareUrl, shareBaseUrl, withKeyFragment } from '../../lib/links';
  import { navigate } from '../../lib/router';
  import { confirmDialog, publicConfig, toast } from '../../lib/stores';
  import { getOwnerShare, setOwnerShare } from '../../lib/tokens';
  import type { Share, ShareFile } from '../../lib/types';
  import { uploadFile } from '../../lib/upload';
  import BarChart from '../../components/BarChart.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';
  import StatusBadge from '../../components/StatusBadge.svelte';

  export let id = '';

  interface Daily {
    day: string;
    views: number;
    downloads: number;
    bytes: number;
  }

  let share: Share | null = null;
  let daily: Daily[] = [];
  let loading = true;
  let error = '';
  let saving = false;
  let busy = '';

  // editable fields
  let title = '';
  let description = '';
  let slug = '';
  let domainId = 0;
  let linkUrl = '';
  let textContent = '';
  let passwordMode: 'keep' | 'new' | 'remove' = 'keep';
  let password = '';
  let expiresAt = '';
  let downloadLimit = 0;
  let status = 'active';

  // manifest (E2EE)
  let manifestTitle = '';
  let manifestNames: Record<string, string> = {};
  let manifestError = '';

  // token rotation result
  let newUrl = '';

  // key rotation
  let rotateOpen = false;
  let rotatePassword = '';
  let rotateBusy = false;
  let rotateProgress = '';
  let rotateResult = '';

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      const res = await api<{ share: Share }>(`/shares/${id}`);
      share = res.share;
      applyShare(res.share);
      try {
        const stats = await api<{ share: Share; daily: Daily[] }>(`/shares/${id}/stats`);
        daily = stats.daily || [];
      } catch {
        daily = [];
      }
      void decodeManifest(res.share);
    } catch (err) {
      error = errorText(err);
    } finally {
      loading = false;
    }
  }

  function applyShare(s: Share): void {
    title = s.title || '';
    description = s.description || '';
    slug = s.slug || '';
    domainId = s.domainId;
    linkUrl = s.linkUrl || '';
    textContent = s.textContent || '';
    expiresAt = s.expiresAt ? toDatetimeLocal(s.expiresAt) : '';
    downloadLimit = s.downloadLimit || 0;
    status = s.status === 'paused' ? 'paused' : 'active';
    passwordMode = 'keep';
    password = '';
  }

  function setPasswordMode(value: string): void {
    passwordMode = value as 'keep' | 'new' | 'remove';
  }

  async function decodeManifest(s: Share): Promise<void> {
    manifestError = '';
    manifestNames = {};
    manifestTitle = getOwnerShare(s.id).title || '';
    if (!s.isE2ee || !s.e2eeManifest) return;
    const record = getOwnerShare(s.id);
    const key = record.key ? masterKeyFromInput(record.key) : null;
    if (!key) {
      manifestError = t('detail.encryptedFile');
      return;
    }
    try {
      const data = await decryptManifest(s.e2eeManifest, key);
      manifestTitle = data.title || manifestTitle;
      manifestNames = {};
      const serverFiles = s.files || [];
      data.files.forEach((file, index) => {
        const serverFile = serverFiles[index];
        if (serverFile) manifestNames[serverFile.id] = file.name;
      });
    } catch {
      manifestError = t('share.decryptFailed');
    }
  }

  function fileLabel(file: ShareFile): string {
    if (share?.isE2ee) return manifestNames[file.id] || `${file.id.slice(0, 8)}…`;
    return file.name || file.id;
  }

  function fileSize(file: ShareFile): number {
    if (share?.isE2ee) return file.encryptedSizeBytes || file.sizeBytes;
    return file.sizeBytes;
  }

  function publicFileUrl(file: ShareFile, inline = false): string {
    if (!share) return '';
    const token = getOwnerShare(share.id).token;
    if (!token || share.passwordSet) return '';
    const base = shareBaseUrl(share.domainHostname);
    return `${base}${share.urlPath}/${token}/files/${file.id}/${inline ? 'inline' : 'download'}`;
  }

  function shareLink(): string {
    if (!share) return '';
    const url = ownerShareUrl(share);
    if (!url) return '';
    const record = getOwnerShare(share.id);
    if (share.isE2ee && share.e2eeMode === 'key' && record.key) {
      return withKeyFragment(url, keyFragment(masterKeyFromInput(record.key) || new Uint8Array()));
    }
    return url;
  }

  async function copyShareLink(): Promise<void> {
    const url = shareLink();
    if (!url) {
      toast(t('shares.linkUnavailable'), 'error');
      return;
    }
    try {
      await copyText(url);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  async function saveSettings(): Promise<void> {
    if (!share) return;
    saving = true;
    try {
      const body: Record<string, unknown> = {
        slug,
        domainId,
        expiresAt: fromDatetimeLocal(expiresAt),
        downloadLimit: Math.max(0, Math.floor(downloadLimit)),
        status
      };
      if (!share.isE2ee) {
        body.title = title;
        body.description = description;
        if (share.type === 'link') body.linkUrl = linkUrl;
        if (share.type === 'text') body.textContent = textContent;
      }
      if (passwordMode === 'new' && password) body.password = password;
      if (passwordMode === 'remove') body.password = '';
      const res = await api<{ share: Share }>(`/shares/${share.id}`, { method: 'PATCH', body });
      share = res.share;
      applyShare(res.share);
      await decodeManifest(res.share);
      toast(t('detail.saved'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      saving = false;
    }
  }

  async function rotateToken(): Promise<void> {
    if (!share) return;
    const ok = await confirmDialog({ title: t('shares.rotateToken'), message: t('shares.rotateTokenConfirm'), confirmLabel: t('shares.rotateToken'), danger: true });
    if (!ok) return;
    busy = 'token';
    try {
      const res = await api<{ token: string; urlPath: string; sharePath: string }>(`/shares/${share.id}/rotate-token`, { method: 'POST', body: {} });
      setOwnerShare(share.id, { token: res.token });
      newUrl = `${shareBaseUrl(share.domainHostname)}${res.urlPath}`;
      toast(t('common.saved'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function trashShare(): Promise<void> {
    if (!share) return;
    const ok = await confirmDialog({ title: t('shares.trashConfirm'), message: share.title || share.slug, confirmLabel: t('shares.trash'), danger: true });
    if (!ok) return;
    busy = 'trash';
    try {
      await api(`/shares/${share.id}`, { method: 'DELETE' });
      toast(t('shares.trashed'), 'success');
      navigate('/shares');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function purgeShare(): Promise<void> {
    if (!share) return;
    const ok = await confirmDialog({ title: t('shares.purgeConfirm'), message: share.title || share.slug, confirmLabel: t('shares.purge'), danger: true });
    if (!ok) return;
    busy = 'purge';
    try {
      await api(`/shares/${share.id}/purge`, { method: 'DELETE' });
      toast(t('common.saved'), 'success');
      navigate('/shares');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function restoreShare(): Promise<void> {
    if (!share) return;
    busy = 'restore';
    try {
      await api(`/shares/${share.id}/restore`, { method: 'POST', body: { expiresAt: 0 } });
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  function openRotate(): void {
    rotateOpen = true;
    rotatePassword = '';
    rotateProgress = '';
    rotateResult = '';
  }

  async function performRotate(): Promise<void> {
    if (!share) return;
    rotateBusy = true;
    rotateProgress = '';
    try {
      const record = getOwnerShare(share.id);
      let oldKey = record.key ? masterKeyFromInput(record.key) : null;
      if (!oldKey && share.e2eeMode === 'password') {
        if (!rotatePassword) {
          throw new Error(t('common.requiredField'));
        }
        oldKey = await unwrapWithPassword(share.e2eeKeyWrap || '', rotatePassword);
      }
      if (!oldKey) {
        if (rotatePassword) {
          oldKey = await unwrapWithRecovery(share.e2eeRecoveryWrap || '', rotatePassword);
        }
      }
      if (!oldKey) throw new Error(t('share.invalidKey'));

      const manifest = await decryptManifest(share.e2eeManifest || '', oldKey);
      rotateProgress = t('create.encrypting');

      const crypto = await createE2EEShareCrypto(share.e2eeMode === 'password' ? 'password' : 'key', rotatePassword);
      const uploadIds: string[] = [];
      let index = 0;
      for (const file of manifest.files) {
        index += 1;
        rotateProgress = `${t('create.encryptingFile')} ${index}/${manifest.files.length}`;
        const blob = await fetchBlob(share.id, file.id);
        const decrypted = await decryptContainerToBlob(blob, oldKey);
        const wrapped = new File([decrypted], file.name || 'file', { type: file.mime || 'application/octet-stream' });
        const result = await uploadFile(wrapped, { encrypted: true, masterKey: crypto.masterKey });
        uploadIds.push(result.id);
      }

      const newManifest = await encryptManifest(
        { ...manifest, title: manifestTitle || manifest.title, files: manifest.files },
        crypto.masterKey
      );
      rotateProgress = t('common.save');
      await api(`/shares/${share.id}/rotate-key`, {
        method: 'POST',
        body: { manifest: newManifest, keyWrap: crypto.keyWrap, recoveryWrap: crypto.recoveryWrap, uploadIds }
      });
      setOwnerShare(share.id, { key: crypto.keyFragment.replace(/^#key=/, ''), title: manifestTitle || manifest.title, e2ee: true, e2eeMode: share.e2eeMode });
      rotateResult = crypto.recoveryKey;
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      rotateBusy = false;
      rotateProgress = '';
    }
  }

  async function fetchBlob(shareId: string, fileId: string): Promise<Blob> {
    const res = await fetch(downloadHref(`/shares/${shareId}/e2ee/${fileId}`), { credentials: 'same-origin' });
    if (!res.ok) throw new Error(t('common.error'));
    return res.blob();
  }

  $: chartData = daily.map((d) => ({ label: d.day.slice(5), value: d.downloads }));
  $: domains = $publicConfig?.domains || [];
  $: editable = share ? share.status !== 'deleted' && share.status !== 'expired' : false;
  $: isTrash = share ? share.status === 'deleted' || share.status === 'expired' : false;
</script>

{#if loading}
  <div class="space-y-4">
    <div class="skeleton h-40"></div>
    <div class="skeleton h-64"></div>
  </div>
{:else if error}
  <div class="card card-pad">
    <p class="flex items-center gap-2 text-sm text-rose-600 dark:text-rose-400"><Icon name="error" size={18} />{error}</p>
    <div class="mt-3 flex gap-2">
      <button type="button" class="btn-secondary btn-sm" on:click={load}><Icon name="refresh" size={15} />{t('common.retry')}</button>
      <button type="button" class="btn-ghost btn-sm" on:click={() => navigate('/shares')}><Icon name="arrowLeft" size={15} />{t('common.back')}</button>
    </div>
  </div>
{:else if share}
  <div class="space-y-6">
    <div class="flex flex-wrap items-center gap-2">
      <button type="button" class="btn-ghost btn-sm" on:click={() => navigate('/shares')}><Icon name="arrowLeft" size={15} />{t('shares.title')}</button>
      <StatusBadge status={share.effectiveStatus || share.status} />
      {#if share.isE2ee}<span class="badge-brand"><Icon name="lock" size={12} />{t('shares.e2ee')}</span>{/if}
      {#if share.passwordSet}<span class="badge-amber"><Icon name="key" size={12} />{t('shares.passwordProtected')}</span>{/if}
      {#if share.adminLocked}<span class="badge-red"><Icon name="lock" size={12} />{t('shares.locked')}</span>{/if}
      {#if share.quarantine}<span class="badge-red"><Icon name="warning" size={12} />{t('shares.quarantined')}</span>{/if}
      <div class="ml-auto flex gap-2">
        <button type="button" class="btn-secondary btn-sm" on:click={copyShareLink}><Icon name="link" size={15} />{t('shares.copyLink')}</button>
        {#if !isTrash}
          <button type="button" class="btn-secondary btn-sm" disabled={busy === 'token'} on:click={rotateToken}><Icon name="rotate" size={15} />{t('shares.rotateToken')}</button>
          {#if share.isE2ee}
            <button type="button" class="btn-secondary btn-sm" on:click={openRotate}><Icon name="key" size={15} />{t('shares.rotateKey')}</button>
          {/if}
          <button type="button" class="btn-danger btn-sm" disabled={busy === 'trash'} on:click={trashShare}><Icon name="trash" size={15} />{t('shares.trash')}</button>
        {:else}
          <button type="button" class="btn-secondary btn-sm" disabled={busy === 'restore'} on:click={restoreShare}><Icon name="restore" size={15} />{t('shares.restore')}</button>
          <button type="button" class="btn-danger btn-sm" disabled={busy === 'purge'} on:click={purgeShare}><Icon name="trash" size={15} />{t('shares.purge')}</button>
        {/if}
      </div>
    </div>

    {#if newUrl}
      <div class="card card-pad flex flex-wrap items-center gap-3 border-brand-200 bg-brand-50/60 dark:border-brand-500/30 dark:bg-brand-500/10">
        <Icon name="check" size={18} class="text-brand-600 dark:text-brand-300" />
        <code class="min-w-0 flex-1 truncate text-sm text-slate-700 dark:text-slate-200">{newUrl}</code>
        <button type="button" class="btn-secondary btn-sm" on:click={() => copyText(newUrl).then(() => toast(t('common.copied'), 'success'))}><Icon name="copy" size={14} />{t('common.copy')}</button>
      </div>
    {/if}

    {#if share.isE2ee}
      <div class="card card-pad">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="lock" size={18} />{t('share.e2eeTitle')}</h2>
        <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{manifestError || t('admin.shares.e2eeNotice')}</p>
        {#if manifestTitle}<p class="mt-2 text-sm font-medium text-slate-700 dark:text-slate-200">{manifestTitle}</p>{/if}
      </div>
    {/if}

    <div class="grid gap-6 lg:grid-cols-3">
      <section class="card card-pad lg:col-span-2">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="settings" size={18} />{t('detail.settings')}</h2>
        <div class="mt-4 grid gap-4 sm:grid-cols-2">
          {#if !share.isE2ee}
            <div class="sm:col-span-2">
              <label class="label" for="d-title">{t('detail.titleField')}</label>
              <input id="d-title" class="input" maxlength="200" bind:value={title} disabled={!editable} />
            </div>
            <div class="sm:col-span-2">
              <label class="label" for="d-desc">{t('detail.descriptionField')}</label>
              <textarea id="d-desc" class="input min-h-[80px]" maxlength="4000" bind:value={description} disabled={!editable}></textarea>
            </div>
            {#if share.type === 'link'}
              <div class="sm:col-span-2">
                <label class="label" for="d-link">{t('detail.linkField')}</label>
                <input id="d-link" class="input" type="url" bind:value={linkUrl} disabled={!editable} />
              </div>
            {/if}
            {#if share.type === 'text'}
              <div class="sm:col-span-2">
                <label class="label" for="d-text">{t('detail.textField')}</label>
                <textarea id="d-text" class="input min-h-[120px]" bind:value={textContent} disabled={!editable}></textarea>
              </div>
            {/if}
          {/if}

          <div>
            <label class="label" for="d-slug">{t('create.slug')}</label>
            <input id="d-slug" class="input" bind:value={slug} disabled={!editable} />
          </div>
          <div>
            <label class="label" for="d-domain">{t('create.domain')}</label>
            <select id="d-domain" class="input" bind:value={domainId} disabled={!editable}>
              {#each domains as domain}
                <option value={domain.id}>{domain.hostname}</option>
              {/each}
            </select>
          </div>
          <div>
            <label class="label" for="d-expiry">{t('detail.expiry')}</label>
            <input id="d-expiry" class="input" type="datetime-local" bind:value={expiresAt} disabled={!editable} />
          </div>
          <div>
            <label class="label" for="d-limit">{t('detail.downloadLimit')}</label>
            <input id="d-limit" class="input" type="number" min="0" bind:value={downloadLimit} disabled={!editable} />
            <p class="hint">{t('shares.downloadLimitHint')}</p>
          </div>
          <div>
            <label class="label" for="d-status">{t('common.status')}</label>
            <select id="d-status" class="input" bind:value={status} disabled={!editable}>
              <option value="active">{t('shares.status.active')}</option>
              <option value="paused">{t('shares.status.paused')}</option>
            </select>
          </div>
          <div>
            <span class="label">{t('create.password')}</span>
            <div class="flex flex-wrap gap-1.5">
              {#each [{ v: 'keep', l: 'detail.passwordKeep' }, { v: 'new', l: 'detail.passwordNew' }, { v: 'remove', l: 'detail.passwordRemove' }] as opt}
                <button
                  type="button"
                  class="rounded-xl px-3 py-1.5 text-xs font-medium transition {passwordMode === opt.v ? 'bg-brand-600 text-white' : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'}"
                  disabled={!editable}
                  on:click={() => setPasswordMode(opt.v)}
                >
                  {t(opt.l)}
                </button>
              {/each}
            </div>
            {#if passwordMode === 'new'}
              <input class="input mt-2" type="password" placeholder={t('create.passwordHint')} bind:value={password} disabled={!editable} />
            {/if}
          </div>
        </div>

        {#if editable}
          <div class="mt-5 flex justify-end">
            <button type="button" class="btn-primary" disabled={saving} on:click={saveSettings}>
              {#if saving}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
              {t('detail.save')}
            </button>
          </div>
        {/if}
      </section>

      <section class="card card-pad">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="chart" size={18} />{t('detail.stats')}</h2>
        <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div><dt class="text-slate-500 dark:text-slate-400">{t('shares.views')}</dt><dd class="font-semibold text-slate-800 dark:text-slate-100">{formatNumber(share.viewCount)}</dd></div>
          <div><dt class="text-slate-500 dark:text-slate-400">{t('shares.downloads')}</dt><dd class="font-semibold text-slate-800 dark:text-slate-100">{formatNumber(share.downloadCount)}</dd></div>
          <div><dt class="text-slate-500 dark:text-slate-400">{t('common.size')}</dt><dd class="font-semibold text-slate-800 dark:text-slate-100">{formatBytes(share.sizeBytes)}</dd></div>
          <div><dt class="text-slate-500 dark:text-slate-400">{t('shares.bytesShort')}</dt><dd class="font-semibold text-slate-800 dark:text-slate-100">{formatBytes(share.bytesServed)}</dd></div>
        </dl>
        <div class="mt-4">
          <p class="mb-2 text-xs font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{t('shares.statsLast30')}</p>
          <BarChart data={chartData} format={(v) => formatNumber(v)} />
        </div>
      </section>
    </div>

    {#if share.type === 'file' || share.type === 'media' || share.type === 'folder'}
      <section class="card card-pad">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="folder" size={18} />{t('detail.files')} <span class="text-sm font-normal text-slate-400">({share.files?.length || 0})</span></h2>
        {#if !share.files || share.files.length === 0}
          <p class="mt-3 text-sm text-slate-500 dark:text-slate-400">{t('admin.shares.noFiles')}</p>
        {:else}
          <ul class="mt-4 divide-y divide-slate-100 dark:divide-slate-800">
            {#each share.files as file}
              <li class="flex items-center gap-3 py-3">
                <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                  <Icon name={share.isE2ee ? 'lock' : 'file'} size={16} />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-medium text-slate-800 dark:text-slate-100">{fileLabel(file)}</span>
                  <span class="block text-xs text-slate-500 dark:text-slate-400">{formatBytes(fileSize(file))} · {file.scanStatus}</span>
                </span>
                {#if !share.isE2ee}
                  {@const url = publicFileUrl(file)}
                  {#if url}
                    <a class="btn-ghost btn-sm" href={url} data-native download><Icon name="download" size={15} /></a>
                  {:else}
                    <span class="text-xs text-slate-400 dark:text-slate-500">{t('shares.linkUnavailable')}</span>
                  {/if}
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    <div class="card card-pad border-rose-200 dark:border-rose-500/30">
      <h2 class="flex items-center gap-2 text-base font-semibold text-rose-700 dark:text-rose-300"><Icon name="warning" size={18} />{t('detail.dangerZone')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('detail.deleteHint')}</p>
      <div class="mt-3 flex flex-wrap gap-2">
        {#if !isTrash}
          <button type="button" class="btn-danger btn-sm" disabled={busy === 'trash'} on:click={trashShare}><Icon name="trash" size={15} />{t('shares.trash')}</button>
        {:else}
          <button type="button" class="btn-secondary btn-sm" disabled={busy === 'restore'} on:click={restoreShare}><Icon name="restore" size={15} />{t('shares.restore')}</button>
          <button type="button" class="btn-danger btn-sm" disabled={busy === 'purge'} on:click={purgeShare}><Icon name="trash" size={15} />{t('shares.purge')}</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<Modal open={rotateOpen} title={t('shares.rotateKey')} subtitle={t('create.e2eeHint')} on:close={() => !rotateBusy && (rotateOpen = false)} closable={!rotateBusy}>
  <div class="space-y-4">
    {#if rotateResult}
      <div class="rounded-xl bg-emerald-50 px-4 py-3 text-sm text-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-200">
        <p class="font-semibold">{t('create.recoveryTitle')}</p>
        <code class="mt-2 block break-all rounded-lg bg-white/70 px-3 py-2 font-mono text-xs dark:bg-slate-900/60">{rotateResult}</code>
        <p class="mt-2 text-xs">{t('create.recoveryHint')}</p>
      </div>
      <div class="flex justify-end">
        <button type="button" class="btn-primary" on:click={() => (rotateOpen = false)}>{t('common.close')}</button>
      </div>
    {:else}
      <div>
        <label class="label" for="rk-secret">{share?.e2eeMode === 'password' ? t('create.e2ee.passwordLabel') : t('share.keyLabel')}</label>
        <input id="rk-secret" class="input" type="password" bind:value={rotatePassword} disabled={rotateBusy} placeholder={share?.e2eeMode === 'password' ? t('create.e2ee.passwordHint') : t('share.e2eeKeyHint')} />
        <p class="hint">{t('create.e2ee.warning')}</p>
      </div>
      {#if rotateProgress}
        <ProgressBar indeterminate label={rotateProgress} />
      {/if}
      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" disabled={rotateBusy} on:click={() => (rotateOpen = false)}>{t('common.cancel')}</button>
        <button type="button" class="btn-primary" disabled={rotateBusy} on:click={performRotate}>
          {#if rotateBusy}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="key" size={15} />{/if}
          {t('shares.rotateKey')}
        </button>
      </div>
    {/if}
  </div>
</Modal>
