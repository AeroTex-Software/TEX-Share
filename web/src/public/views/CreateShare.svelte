<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { randomId, webCryptoAvailable } from '../../lib/crypto';
  import { createE2EEShareCrypto, encryptManifest, keyFragment } from '../../lib/e2ee';
  import { formatBytes, formatDateTime, fromDatetimeLocal, toDatetimeLocal } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { shareBaseUrl, withKeyFragment } from '../../lib/links';
  import { navigate } from '../../lib/router';
  import { loadPublicConfig, publicConfig, toast } from '../../lib/stores';
  import { setOwnerShare } from '../../lib/tokens';
  import type { Share } from '../../lib/types';
  import { cancelUpload, uploadFile, type UploadProgress, type UploadResult } from '../../lib/upload';
  import Icon from '../../components/Icon.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';

  type ShareType = 'file' | 'folder' | 'link' | 'text' | 'media';
  type E2EEMode = 'key' | 'password';
  type Step = 'type' | 'options' | 'content' | 'review' | 'done';

  interface Picked {
    file: File;
    id: string;
    progress: UploadProgress;
    result: UploadResult | null;
    error: string;
  }

  const typeOptions: { value: ShareType; icon: string; label: string; desc: string }[] = [
    { value: 'file', icon: 'file', label: 'create.type.file', desc: 'create.type.file.desc' },
    { value: 'folder', icon: 'folder', label: 'create.type.folder', desc: 'create.type.folder.desc' },
    { value: 'media', icon: 'media', label: 'create.type.media', desc: 'create.type.media.desc' },
    { value: 'link', icon: 'link', label: 'create.type.link', desc: 'create.type.link.desc' },
    { value: 'text', icon: 'text', label: 'create.type.text', desc: 'create.type.text.desc' }
  ];

  let step: Step = 'type';
  let shareType: ShareType = 'file';

  let title = '';
  let description = '';
  let slug = '';
  let domainId = 0;
  let linkUrl = '';
  let textContent = '';
  let password = '';
  let expiresAt = '';
  let downloadLimit = 0;

  let e2eeEnabled = false;
  let e2eeMode: E2EEMode = 'key';
  let e2eePassword = '';
  let e2eePasswordConfirm = '';

  let picked: Picked[] = [];
  let uploading = false;
  let dragOver = false;

  let creating = false;
  let createdUrl = '';
  let recoveryKey = '';
  let createdShare: Share | null = null;

  let fileInput: HTMLInputElement | null = null;

  onMount(async () => {
    if (!$publicConfig) await loadPublicConfig();
    const domains = $publicConfig?.domains || [];
    const def = domains.find((d) => d.isDefault) || domains[0];
    if (def) domainId = def.id;
  });

  $: domains = $publicConfig?.domains || [];
  $: limits = $publicConfig?.limits;
  $: needsFiles = shareType === 'file' || shareType === 'folder' || shareType === 'media';
  $: selectedCount = picked.length;
  $: totalPlain = picked.reduce((sum, p) => sum + p.file.size, 0);
  $: allUploaded = picked.length > 0 && picked.every((p) => p.result);
  $: anyFailed = picked.some((p) => p.error);
  $: e2eeAvailable = webCryptoAvailable();

  function chooseType(value: ShareType): void {
    shareType = value;
    if (value === 'file') picked = picked.slice(0, 1);
  }

  function nextFromType(): void {
    step = 'options';
  }

  function nextFromOptions(): void {
    if (e2eeEnabled && e2eeMode === 'password') {
      if (e2eePassword.length < 8) {
        toast(t('create.e2ee.passwordHint'), 'error');
        return;
      }
      if (e2eePassword !== e2eePasswordConfirm) {
        toast(t('create.e2ee.passwordMismatch'), 'error');
        return;
      }
    }
    step = 'content';
  }

  function pickFiles(event: Event): void {
    const input = event.target as HTMLInputElement;
    addFiles(input.files ? Array.from(input.files) : []);
    input.value = '';
  }

  function addFiles(files: File[]): void {
    if (!files.length) return;
    const max = limits?.maxFileSizeBytes || 0;
    const accepted: Picked[] = [];
    for (const file of files) {
      if (max > 0 && file.size > max) {
        toast(`${file.name}: ${t('create.fileTooLarge')}`, 'error');
        continue;
      }
      accepted.push({ file, id: randomId(), progress: { loaded: 0, total: file.size, phase: 'uploading' }, result: null, error: '' });
    }
    let next = [...picked, ...accepted];
    if (shareType === 'file') next = next.slice(0, 1);
    const shareMax = limits?.maxShareSizeBytes || 0;
    if (shareMax > 0) {
      let sum = 0;
      const limited: Picked[] = [];
      for (const p of next) {
        if (sum + p.file.size > shareMax) {
          toast(t('create.totalTooLarge'), 'error');
          break;
        }
        sum += p.file.size;
        limited.push(p);
      }
      next = limited;
    }
    picked = next;
  }

  function removeFile(id: string): void {
    const target = picked.find((p) => p.id === id);
    if (target?.result) void cancelUpload(target.result.id).catch(() => undefined);
    picked = picked.filter((p) => p.id !== id);
  }

  function onDrop(event: DragEvent): void {
    event.preventDefault();
    dragOver = false;
    const files = event.dataTransfer?.files ? Array.from(event.dataTransfer.files) : [];
    addFiles(files);
  }

  async function runUploads(): Promise<void> {
    if (uploading || !picked.length) return;
    if (e2eeEnabled && !e2eeAvailable) {
      toast(t('create.e2ee.unavailable'), 'error');
      return;
    }
    uploading = true;
    let masterKey: Uint8Array | null = null;
    try {
      if (e2eeEnabled) {
        const crypto = await createE2EEShareCrypto(e2eeMode, e2eePassword);
        masterKey = crypto.masterKey;
        (window as unknown as { __texE2EE?: unknown }).__texE2EE = crypto;
      }
      for (const item of picked) {
        if (item.result) continue;
        item.error = '';
        try {
          const result = await uploadFile(item.file, {
            encrypted: e2eeEnabled,
            masterKey: masterKey || undefined,
            onProgress: (progress) => {
              item.progress = progress;
              picked = [...picked];
            }
          });
          item.result = result;
        } catch (err) {
          item.error = errorText(err);
        }
        picked = [...picked];
      }
    } finally {
      uploading = false;
    }
  }

  function nextFromContent(): void {
    if (needsFiles) {
      if (!picked.length) {
        toast(t('create.noFiles'), 'error');
        return;
      }
      if (!allUploaded) {
        toast(t('create.uploading'), 'error');
        return;
      }
      if (shareType === 'file' && picked.length !== 1) {
        toast(t('err.exactly_one_file_required'), 'error');
        return;
      }
    } else if (shareType === 'link') {
      if (!/^https?:\/\//i.test(linkUrl.trim())) {
        toast(t('create.invalidLink'), 'error');
        return;
      }
    } else if (shareType === 'text') {
      if (!textContent.trim()) {
        toast(t('create.textRequired'), 'error');
        return;
      }
    }
    step = 'review';
  }

  function setExpiry(days: number): void {
    if (days <= 0) {
      expiresAt = '';
      return;
    }
    expiresAt = toDatetimeLocal(Math.floor(Date.now() / 1000) + days * 86400);
  }

  async function create(): Promise<void> {
    if (creating) return;
    if (e2eeEnabled && !e2eeAvailable) {
      toast(t('create.e2ee.unavailable'), 'error');
      return;
    }
    creating = true;
    try {
      const body: Record<string, unknown> = {
        type: shareType,
        domainId,
        slug: slug.trim(),
        password,
        expiresAt: fromDatetimeLocal(expiresAt),
        downloadLimit: Math.max(0, Math.floor(downloadLimit)),
        uploadIds: picked.filter((p) => p.result).map((p) => p.result!.id)
      };
      if (e2eeEnabled) {
        const crypto = (window as unknown as { __texE2EE?: Awaited<ReturnType<typeof createE2EEShareCrypto>> }).__texE2EE;
        if (!crypto) throw new Error(t('create.failed'));
        const manifest = await encryptManifest(
          {
            v: 1,
            title: title.trim(),
            description: description.trim(),
            type: shareType,
            linkUrl: shareType === 'link' ? linkUrl.trim() : undefined,
            textContent: shareType === 'text' ? textContent : undefined,
            files: picked
              .filter((p) => p.result)
              .map((p) => ({ id: p.result!.id, name: p.file.name, mime: p.file.type || 'application/octet-stream', size: p.file.size }))
          },
          crypto.masterKey
        );
        body.e2ee = { mode: e2eeMode, manifest, keyWrap: crypto.keyWrap, recoveryWrap: crypto.recoveryWrap };
      } else {
        body.title = title.trim();
        body.description = description.trim();
        if (shareType === 'link') body.linkUrl = linkUrl.trim();
        if (shareType === 'text') body.textContent = textContent;
      }
      const res = await api<{ share: Share }>('/shares', { method: 'POST', body });
      createdShare = res.share;
      const token = res.share.token || '';
      setOwnerShare(res.share.id, { token, title: title.trim() || res.share.slug, type: shareType, e2ee: e2eeEnabled, e2eeMode: e2eeEnabled ? e2eeMode : undefined });
      let url = `${shareBaseUrl(res.share.domainHostname)}${res.share.urlPath || res.share.sharePath || ''}/${token}`;
      if (e2eeEnabled) {
        const crypto = (window as unknown as { __texE2EE?: Awaited<ReturnType<typeof createE2EEShareCrypto>> }).__texE2EE;
        if (crypto) {
          setOwnerShare(res.share.id, { key: crypto.keyFragment.replace(/^#key=/, '') });
          recoveryKey = crypto.recoveryKey;
          if (e2eeMode === 'key') url = withKeyFragment(url, keyFragment(crypto.masterKey));
        }
      }
      createdUrl = url;
      step = 'done';
      toast(t('create.done'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      creating = false;
    }
  }

  async function copyCreated(): Promise<void> {
    try {
      await copyText(createdUrl);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  function setE2eeMode(value: string): void {
    e2eeMode = value as E2EEMode;
  }

  function openCreated(): void {
    if (createdShare) navigate(`/shares/${createdShare.id}`);
  }

  function reset(): void {
    step = 'type';
    shareType = 'file';
    title = '';
    description = '';
    slug = '';
    linkUrl = '';
    textContent = '';
    password = '';
    expiresAt = '';
    downloadLimit = 0;
    e2eeEnabled = false;
    e2eeMode = 'key';
    e2eePassword = '';
    e2eePasswordConfirm = '';
    picked = [];
    createdUrl = '';
    recoveryKey = '';
    createdShare = null;
  }

  $: steps = needsFiles
    ? ['type', 'options', 'content', 'review']
    : ['type', 'options', 'content', 'review'];
  $: stepIndex = steps.indexOf(step);
</script>

<div class="space-y-6">
  {#if step !== 'done'}
    <ol class="flex flex-wrap items-center gap-2 text-xs font-medium">
      {#each steps as item, index}
        <li class="flex items-center gap-2">
          <span class="flex h-6 w-6 items-center justify-center rounded-full {index <= stepIndex ? 'bg-brand-600 text-white' : 'bg-slate-200 text-slate-500 dark:bg-slate-800 dark:text-slate-400'}">
            {index + 1}
          </span>
          <span class={index === stepIndex ? 'text-slate-900 dark:text-white' : 'text-slate-400 dark:text-slate-500'}>{t(`create.step.${item}`)}</span>
          {#if index < steps.length - 1}<Icon name="chevronRight" size={14} class="text-slate-300 dark:text-slate-600" />{/if}
        </li>
      {/each}
    </ol>
  {/if}

  {#if step === 'type'}
    <section class="card card-pad">
      <h2 class="text-lg font-semibold text-slate-900 dark:text-white">{t('create.title')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('create.subtitle')}</p>
      <div class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {#each typeOptions as opt}
          <button
            type="button"
            class="flex items-start gap-3 rounded-2xl border p-4 text-left transition {shareType === opt.value
              ? 'border-brand-500 bg-brand-50 dark:border-brand-500/50 dark:bg-brand-500/10'
              : 'border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700'}"
            on:click={() => chooseType(opt.value)}
          >
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
              <Icon name={opt.icon} size={19} />
            </span>
            <span class="min-w-0">
              <span class="block text-sm font-semibold text-slate-800 dark:text-slate-100">{t(opt.label)}</span>
              <span class="mt-0.5 block text-xs text-slate-500 dark:text-slate-400">{t(opt.desc)}</span>
            </span>
          </button>
        {/each}
      </div>
      <div class="mt-5 flex justify-end">
        <button type="button" class="btn-primary" on:click={nextFromType}>{t('common.next')}<Icon name="arrowRight" size={16} /></button>
      </div>
    </section>
  {:else if step === 'options'}
    <section class="card card-pad">
      <h2 class="text-lg font-semibold text-slate-900 dark:text-white">{t('create.step.options')}</h2>
      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <div class="sm:col-span-2">
          <label class="label" for="c-title">{t('create.titleLabel')}</label>
          <input id="c-title" class="input" maxlength="200" placeholder={t('create.titlePlaceholder')} bind:value={title} />
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="c-desc">{t('create.description')}</label>
          <textarea id="c-desc" class="input min-h-[80px]" maxlength="4000" bind:value={description}></textarea>
        </div>
        <div>
          <label class="label" for="c-slug">{t('create.slug')}</label>
          <input id="c-slug" class="input" bind:value={slug} />
          <p class="hint">{t('create.slugHint')}</p>
        </div>
        <div>
          <label class="label" for="c-domain">{t('create.domain')}</label>
          <select id="c-domain" class="input" bind:value={domainId}>
            {#each domains as domain}
              <option value={domain.id}>{domain.hostname}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label" for="c-expiry">{t('create.expiry')}</label>
          <input id="c-expiry" class="input" type="datetime-local" bind:value={expiresAt} />
          <div class="mt-1.5 flex flex-wrap gap-1.5">
            <button type="button" class="btn-ghost btn-sm" on:click={() => setExpiry(1)}>1 {t('common.days')}</button>
            <button type="button" class="btn-ghost btn-sm" on:click={() => setExpiry(7)}>7 {t('common.days')}</button>
            <button type="button" class="btn-ghost btn-sm" on:click={() => setExpiry(30)}>30 {t('common.days')}</button>
            <button type="button" class="btn-ghost btn-sm" on:click={() => setExpiry(0)}>{t('common.never')}</button>
          </div>
          <p class="hint">{t('create.expiryHint')}</p>
        </div>
        <div>
          <label class="label" for="c-limit">{t('create.downloadLimit')}</label>
          <input id="c-limit" class="input" type="number" min="0" bind:value={downloadLimit} />
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="c-password">{t('create.password')}</label>
          <input id="c-password" class="input" type="password" autocomplete="new-password" bind:value={password} placeholder={t('create.passwordHint')} />
        </div>
      </div>

      <div class="mt-6 rounded-2xl border border-slate-200 p-4 dark:border-slate-800">
        <label class="flex items-start gap-3 {e2eeAvailable ? 'cursor-pointer' : 'cursor-not-allowed opacity-70'}">
          <input
            type="checkbox"
            class="mt-1 h-4 w-4 rounded border-slate-300 text-brand-600 focus:ring-brand-500 disabled:cursor-not-allowed dark:border-slate-700 dark:bg-slate-900"
            bind:checked={e2eeEnabled}
            disabled={!e2eeAvailable}
          />
          <span>
            <span class="flex items-center gap-2 text-sm font-semibold text-slate-800 dark:text-slate-100"><Icon name="lock" size={15} />{t('create.e2ee')}</span>
            <span class="mt-0.5 block text-xs text-slate-500 dark:text-slate-400">{t('create.e2eeHint')}</span>
          </span>
        </label>

        {#if !e2eeAvailable}
          <p class="mt-3 flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">
            <Icon name="warning" size={14} class="mt-0.5" />{t('create.e2ee.unavailable')}
          </p>
        {/if}

        {#if e2eeEnabled && e2eeAvailable}
          <div class="mt-4 space-y-3 border-t border-slate-100 pt-4 dark:border-slate-800">
            <div>
              <span class="label">{t('create.e2ee.mode')}</span>
              <div class="flex flex-wrap gap-1.5">
                {#each [{ v: 'key', l: 'create.modeKey' }, { v: 'password', l: 'create.modePassword' }] as opt}
                  <button
                    type="button"
                    class="rounded-xl px-3 py-1.5 text-xs font-medium transition {e2eeMode === opt.v ? 'bg-brand-600 text-white' : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'}"
                    on:click={() => setE2eeMode(opt.v)}
                  >
                    {t(opt.l)}
                  </button>
                {/each}
              </div>
            </div>
            {#if e2eeMode === 'key'}
              <p class="flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">
                <Icon name="warning" size={14} class="mt-0.5" />{t('create.e2ee.keyHint')}
              </p>
            {:else}
              <div class="grid gap-3 sm:grid-cols-2">
                <div>
                  <label class="label" for="e2ee-pass">{t('create.e2ee.passwordLabel')}</label>
                  <input id="e2ee-pass" class="input" type="password" autocomplete="new-password" bind:value={e2eePassword} />
                </div>
                <div>
                  <label class="label" for="e2ee-pass2">{t('create.e2ee.passwordConfirm')}</label>
                  <input id="e2ee-pass2" class="input" type="password" autocomplete="new-password" bind:value={e2eePasswordConfirm} />
                </div>
              </div>
              <p class="hint">{t('create.e2ee.passwordHint')}</p>
            {/if}
            <p class="flex items-start gap-2 rounded-xl bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:bg-slate-800/60 dark:text-slate-300">
              <Icon name="info" size={14} class="mt-0.5" />{t('create.e2ee.warning')}
            </p>
          </div>
        {/if}
      </div>

      <div class="mt-5 flex justify-between">
        <button type="button" class="btn-secondary" on:click={() => (step = 'type')}><Icon name="arrowLeft" size={16} />{t('common.back')}</button>
        <button type="button" class="btn-primary" on:click={nextFromOptions}>{t('common.next')}<Icon name="arrowRight" size={16} /></button>
      </div>
    </section>
  {:else if step === 'content'}
    <section class="card card-pad">
      <h2 class="text-lg font-semibold text-slate-900 dark:text-white">{t('create.step.files')}</h2>

      {#if needsFiles}
        <div
          class="mt-4 flex flex-col items-center justify-center gap-2 rounded-2xl border-2 border-dashed px-6 py-10 text-center transition {dragOver
            ? 'border-brand-500 bg-brand-50 dark:bg-brand-500/10'
            : 'border-slate-300 dark:border-slate-700'}"
          role="button"
          tabindex="0"
          on:dragover|preventDefault={() => (dragOver = true)}
          on:dragleave={() => (dragOver = false)}
          on:drop={onDrop}
          on:click={() => fileInput?.click()}
          on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && fileInput?.click()}
        >
          <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500"><Icon name="upload" size={22} /></span>
          <p class="text-sm font-medium text-slate-700 dark:text-slate-200">{t('create.dropFiles')}</p>
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('create.dropHint')}</p>
          <input
            bind:this={fileInput}
            type="file"
            class="hidden"
            multiple={shareType !== 'file'}
            on:change={pickFiles}
          />
        </div>

        {#if picked.length}
          <ul class="mt-4 space-y-3">
            {#each picked as item (item.id)}
              <li class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
                <div class="flex items-center gap-3">
                  <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300"><Icon name="file" size={16} /></span>
                  <span class="min-w-0 flex-1">
                    <span class="block truncate text-sm font-medium text-slate-800 dark:text-slate-100">{item.file.name}</span>
                    <span class="block text-xs text-slate-500 dark:text-slate-400">
                      {formatBytes(item.file.size)}
                      {#if item.result}· <span class="text-emerald-600 dark:text-emerald-400">{t('create.uploaded') || 'OK'}</span>{/if}
                    </span>
                  </span>
                  {#if !uploading && !item.result}
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={() => removeFile(item.id)}><Icon name="trash" size={15} /></button>
                  {/if}
                </div>
                {#if !item.result}
                  <div class="mt-2">
                    <ProgressBar
                      value={item.progress.phase === 'encrypting' ? item.progress.loaded * 0.3 : item.progress.loaded}
                      max={Math.max(1, item.progress.total)}
                      label={item.progress.phase === 'encrypting' ? t('create.encrypting') : t('create.uploading')}
                      tone={item.error ? 'rose' : 'brand'}
                    />
                  </div>
                {/if}
                {#if item.error}
                  <p class="mt-2 text-xs text-rose-600 dark:text-rose-400">{item.error}</p>
                {/if}
              </li>
            {/each}
          </ul>
          {#if totalPlain > 0}
            <p class="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('create.selectedFiles')}: {selectedCount} · {formatBytes(totalPlain)}</p>
          {/if}
        {/if}
      {:else if shareType === 'link'}
        <div class="mt-4">
          <label class="label" for="c-link">{t('create.linkUrl')}</label>
          <input id="c-link" class="input" type="url" placeholder="https://" bind:value={linkUrl} />
          <p class="hint">{t('create.linkUrlHint')}</p>
        </div>
      {:else}
        <div class="mt-4">
          <label class="label" for="c-text">{t('create.textContent')}</label>
          <textarea id="c-text" class="input min-h-[200px] font-mono" bind:value={textContent}></textarea>
        </div>
      {/if}

      <div class="mt-5 flex flex-wrap justify-between gap-2">
        <button type="button" class="btn-secondary" disabled={uploading} on:click={() => (step = 'options')}><Icon name="arrowLeft" size={16} />{t('common.back')}</button>
        <div class="flex gap-2">
          {#if needsFiles}
            <button type="button" class="btn-secondary" disabled={uploading || !picked.length || allUploaded} on:click={runUploads}>
              {#if uploading}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="upload" size={16} />{/if}
              {uploading ? t('create.uploading') : t('create.chooseFiles')}
            </button>
          {/if}
          <button type="button" class="btn-primary" disabled={uploading} on:click={nextFromContent}>{t('common.next')}<Icon name="arrowRight" size={16} /></button>
        </div>
      </div>
    </section>
  {:else if step === 'review'}
    <section class="card card-pad">
      <h2 class="text-lg font-semibold text-slate-900 dark:text-white">{t('create.review')}</h2>
      <dl class="mt-4 space-y-2 text-sm">
        <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('common.type')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{t(`create.type.${shareType}`)}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.titleLabel')}</dt><dd class="truncate font-medium text-slate-800 dark:text-slate-100">{title || '—'}</dd></div>
        {#if needsFiles}
          <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.summaryFiles')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{selectedCount} · {formatBytes(totalPlain)}</dd></div>
        {/if}
        {#if shareType === 'link'}
          <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.linkUrl')}</dt><dd class="truncate font-medium text-slate-800 dark:text-slate-100">{linkUrl}</dd></div>
        {/if}
        <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.summaryPassword')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{password ? t('common.yes') : t('common.no')}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.summaryExpiry')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{expiresAt ? formatDateTime(fromDatetimeLocal(expiresAt)) : t('create.summaryNoExpiry')}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-slate-500 dark:text-slate-400">{t('create.summaryE2EE')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{e2eeEnabled ? t(`create.mode${e2eeMode === 'key' ? 'Key' : 'Password'}`) : t('common.no')}</dd></div>
      </dl>

      <div class="mt-5 flex justify-between">
        <button type="button" class="btn-secondary" disabled={creating} on:click={() => (step = 'content')}><Icon name="arrowLeft" size={16} />{t('common.back')}</button>
        <button type="button" class="btn-primary" disabled={creating} on:click={create}>
          {#if creating}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {creating ? t('create.creating') : t('create.submit')}
        </button>
      </div>
    </section>
  {:else}
    <section class="card card-pad text-center">
      <span class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-emerald-100 text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-300"><Icon name="check" size={26} /></span>
      <h2 class="mt-4 text-lg font-semibold text-slate-900 dark:text-white">{t('create.successTitle')}</h2>
      <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('create.successHint')}</p>

      <div class="mx-auto mt-4 flex max-w-xl items-center gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 dark:border-slate-800 dark:bg-slate-900">
        <code class="min-w-0 flex-1 truncate text-left text-xs text-slate-700 dark:text-slate-200">{createdUrl}</code>
        <button type="button" class="btn-secondary btn-sm shrink-0" on:click={copyCreated}><Icon name="copy" size={14} />{t('create.copyLink')}</button>
      </div>

      {#if recoveryKey}
        <div class="mx-auto mt-4 max-w-xl rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-left dark:border-amber-500/30 dark:bg-amber-500/10">
          <p class="flex items-center gap-2 text-sm font-semibold text-amber-800 dark:text-amber-200"><Icon name="key" size={15} />{t('create.recoveryTitle')}</p>
          <code class="mt-2 block break-all rounded-lg bg-white/70 px-3 py-2 font-mono text-xs text-amber-900 dark:bg-slate-900/60 dark:text-amber-100">{recoveryKey}</code>
          <p class="mt-2 text-xs text-amber-700 dark:text-amber-300">{t('create.recoveryHint')}</p>
        </div>
      {/if}

      <div class="mt-5 flex flex-wrap justify-center gap-2">
        <button type="button" class="btn-secondary" on:click={reset}><Icon name="plus" size={16} />{t('create.title')}</button>
        {#if createdShare}
          <button type="button" class="btn-primary" on:click={openCreated}>{t('create.openShare')}<Icon name="arrowRight" size={16} /></button>
        {/if}
      </div>
    </section>
  {/if}
</div>
