<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { formatBytes, formatDate, percent } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { confirmDialog, patchUser, toast, user as sessionUser } from '../../lib/stores';
  import type { Domain, Share, User } from '../../lib/types';
  import Avatar from '../../components/Avatar.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';
  import Toggle from '../../components/Toggle.svelte';

  interface AdminUser extends User {
    shareCount: number;
    domains: number[];
    storagePercent: number;
    totalShares?: number;
  }

  let users: AdminUser[] = [];
  let total = 0;
  let page = 1;
  const limit = 25;
  let query = '';
  let roleFilter = '';
  let loading = true;
  let domains: Domain[] = [];

  // Create / edit
  let editorOpen = false;
  let editing: AdminUser | null = null;
  let form = { username: '', displayName: '', password: '', role: 'user', namespace: '', storageLimitGb: 0, unlimited: false, domainIds: [] as number[] };
  let saving = false;
  let formError = '';

  // Temp password result
  let tempOpen = false;
  let tempPassword = '';

  // Delete wizard
  let deleteOpen = false;
  let deleteUser: AdminUser | null = null;
  let deleteShares: Share[] = [];
  let deleteOthers: AdminUser[] = [];
  let deleteConfirm = '';
  let deleteDefaultAction: 'trash' | 'transfer' = 'trash';
  let deleteTransferTo = 0;
  let deleteAssignments: Record<string, { action: 'trash' | 'transfer'; targetUserId: number }> = {};
  let deleteBusy = false;
  let deleteError = '';

  onMount(async () => {
    await Promise.all([load(), loadDomains()]);
  });

  async function load(): Promise<void> {
    loading = true;
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) });
      if (query.trim()) params.set('q', query.trim());
      if (roleFilter) params.set('role', roleFilter);
      const res = await api<{ users: AdminUser[]; total: number }>(`/users?${params.toString()}`);
      users = res.users || [];
      total = res.total || 0;
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  async function loadDomains(): Promise<void> {
    try {
      const res = await api<{ domains: Domain[] }>('/domains');
      domains = res.domains || [];
    } catch {
      // Domain assignment is optional.
    }
  }

  function search(): void {
    page = 1;
    void load();
  }

  function changePage(next: number): void {
    page = next;
    void load();
  }

  function openCreate(): void {
    editing = null;
    form = { username: '', displayName: '', password: '', role: 'user', namespace: '', storageLimitGb: 0, unlimited: false, domainIds: [] };
    formError = '';
    editorOpen = true;
  }

  function openEdit(item: AdminUser): void {
    editing = item;
    form = {
      username: item.username,
      displayName: item.displayName,
      password: '',
      role: item.role,
      namespace: item.namespace || '',
      storageLimitGb: item.storageLimitBytes > 0 ? Math.round(item.storageLimitBytes / (1024 * 1024 * 1024)) : 0,
      unlimited: item.storageLimitBytes === 0,
      domainIds: [...(item.domains || [])]
    };
    formError = '';
    editorOpen = true;
  }

  function toggleDomain(id: number): void {
    form.domainIds = form.domainIds.includes(id) ? form.domainIds.filter((d) => d !== id) : [...form.domainIds, id];
  }

  async function saveUser(): Promise<void> {
    formError = '';
    const displayName = form.displayName.trim() || form.username.trim();
    if (!editing && !/^[A-Za-z0-9._-]{3,32}$/.test(form.username.trim())) {
      formError = t('err.invalid_username');
      return;
    }
    if (displayName.length < 1 || displayName.length > 80) {
      formError = t('err.invalid_display_name');
      return;
    }
    const storageLimitBytes = form.unlimited ? 0 : Math.max(0, Math.round(form.storageLimitGb * 1024 * 1024 * 1024));
    saving = true;
    try {
      if (editing) {
        const res = await api<{ user: AdminUser }>(`/users/${editing.id}`, {
          method: 'PATCH',
          body: {
            displayName,
            role: form.role,
            namespace: form.namespace.trim(),
            storageLimitBytes,
            domainIds: form.domainIds
          }
        });
        if (editing.id === $sessionUser?.id && res.user) {
          patchUser({ displayName: res.user.displayName, role: res.user.role });
        }
        toast(t('admin.users.saved'), 'success');
      } else {
        const res = await api<{ user: AdminUser; temporaryPassword: string }>('/users', {
          method: 'POST',
          body: {
            username: form.username.trim(),
            displayName,
            password: form.password,
            role: form.role,
            namespace: form.namespace.trim(),
            storageLimitBytes,
            domainIds: form.domainIds
          }
        });
        tempPassword = res.temporaryPassword;
        tempOpen = true;
      }
      editorOpen = false;
      await load();
    } catch (err) {
      formError = errorText(err);
    } finally {
      saving = false;
    }
  }

  async function copyTemp(): Promise<void> {
    try {
      await copyText(tempPassword);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  async function suspend(item: AdminUser): Promise<void> {
    const ok = await confirmDialog({
      title: t('admin.users.suspend'),
      message: t('admin.users.suspendConfirm', { username: item.username }),
      confirmLabel: t('admin.users.suspend'),
      danger: true
    });
    if (!ok) return;
    try {
      await api(`/users/${item.id}/suspend`, { method: 'POST', body: { reason: '' } });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function reactivate(item: AdminUser): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.users.reactivate'), message: t('admin.users.reactivateConfirm', { username: item.username }) });
    if (!ok) return;
    try {
      await api(`/users/${item.id}/reactivate`, { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function resetPassword(item: AdminUser): Promise<void> {
    const ok = await confirmDialog({
      title: t('admin.users.resetPassword'),
      message: t('admin.users.resetPasswordConfirm', { username: item.username }),
      danger: true
    });
    if (!ok) return;
    try {
      const res = await api<{ temporaryPassword: string }>(`/users/${item.id}/reset-password`, { method: 'POST', body: {} });
      tempPassword = res.temporaryPassword;
      tempOpen = true;
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function reset2fa(item: AdminUser): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.users.reset2fa'), message: t('admin.users.reset2faConfirm', { username: item.username }), danger: true });
    if (!ok) return;
    try {
      await api(`/users/${item.id}/reset-2fa`, { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function revokeSessions(item: AdminUser): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.users.revokeSessions'), message: t('admin.users.revokeConfirm', { username: item.username }) });
    if (!ok) return;
    try {
      await api(`/users/${item.id}/revoke-sessions`, { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function openDelete(item: AdminUser): Promise<void> {
    deleteUser = item;
    deleteConfirm = '';
    deleteDefaultAction = 'trash';
    deleteTransferTo = 0;
    deleteAssignments = {};
    deleteError = '';
    deleteShares = [];
    deleteOthers = [];
    deleteOpen = true;
    try {
      const [preview, list] = await Promise.all([
        api<{ shares: Share[] }>(`/users/${item.id}/delete-preview`),
        api<{ users: AdminUser[] }>('/users?limit=200')
      ]);
      deleteShares = preview.shares || [];
      deleteOthers = (list.users || []).filter((u) => u.id !== item.id && !u.suspended);
      for (const share of deleteShares) {
        deleteAssignments[share.id] = {
          action: share.status === 'deleted' || share.status === 'expired' ? 'trash' : deleteDefaultAction,
          targetUserId: 0
        };
      }
    } catch (err) {
      deleteError = errorText(err);
    }
  }

  function applyDefault(): void {
    for (const share of deleteShares) {
      if (share.status === 'deleted' || share.status === 'expired') {
        deleteAssignments[share.id] = { action: 'trash', targetUserId: 0 };
      } else {
        deleteAssignments[share.id] = { action: deleteDefaultAction, targetUserId: deleteTransferTo };
      }
    }
  }

  $: deleteReady =
    deleteUser !== null &&
    deleteConfirm.trim() === deleteUser.username &&
    deleteShares.every((s) => {
      const a = deleteAssignments[s.id];
      return a && (a.action === 'trash' || a.targetUserId > 0);
    }) &&
    !deleteBusy;

  async function submitDelete(): Promise<void> {
    if (!deleteUser || !deleteReady) return;
    deleteBusy = true;
    deleteError = '';
    try {
      await api(`/users/${deleteUser.id}`, {
        method: 'DELETE',
        body: {
          confirmUsername: deleteConfirm.trim(),
          defaultAction: deleteDefaultAction,
          transferTo: deleteTransferTo,
          assignments: Object.entries(deleteAssignments).map(([shareId, a]) => ({
            shareId,
            action: a.action,
            targetUserId: a.targetUserId
          }))
        }
      });
      deleteOpen = false;
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      deleteError = errorText(err);
    } finally {
      deleteBusy = false;
    }
  }

  function storageLabel(item: AdminUser): string {
    if (item.storageLimitBytes === 0) return t('common.unlimited');
    return `${formatBytes(item.storageLimitBytes)}`;
  }
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center gap-2">
    <div class="relative min-w-[12rem] flex-1">
      <span class="pointer-events-none absolute inset-y-0 left-0 flex w-10 items-center justify-center text-slate-400"><Icon name="search" size={16} /></span>
      <input class="input pl-10" placeholder={t('admin.users.search')} bind:value={query} on:keydown={(e) => e.key === 'Enter' && search()} />
    </div>
    <select class="input w-auto" bind:value={roleFilter} on:change={search}>
      <option value="">{t('common.all')}</option>
      <option value="admin">{t('profile.role.admin')}</option>
      <option value="user">{t('profile.role.user')}</option>
    </select>
    <button type="button" class="btn-secondary" on:click={search}><Icon name="search" size={16} />{t('common.search')}</button>
    <button type="button" class="btn-primary" on:click={openCreate}><Icon name="plus" size={16} />{t('admin.users.new')}</button>
  </div>

  {#if loading}
    <div class="space-y-2">{#each Array(6) as _}<div class="skeleton h-16"></div>{/each}</div>
  {:else if users.length === 0}
    <div class="card"><EmptyState icon="users" title={t('admin.users.title')} message={t('admin.users.subtitle')} /></div>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{t('admin.users.username')}</th>
            <th>{t('admin.users.role')}</th>
            <th>{t('admin.users.storage')}</th>
            <th>{t('admin.users.status')}</th>
            <th>{t('admin.users.created')}</th>
            <th class="text-right">{t('admin.users.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {#each users as item (item.id)}
            <tr>
              <td>
                <div class="flex items-center gap-3">
                  <Avatar user={item} size={36} />
                  <div class="min-w-0">
                    <p class="truncate font-medium text-slate-800 dark:text-slate-100">{item.displayName || item.username}</p>
                    <p class="truncate text-xs text-slate-500 dark:text-slate-400">@{item.username}{item.namespace ? ` · ${item.namespace}` : ''}</p>
                  </div>
                </div>
              </td>
              <td>
                {#if item.role === 'admin'}<span class="badge-brand">{t('profile.role.admin')}</span>{:else}<span class="badge-slate">{t('profile.role.user')}</span>{/if}
              </td>
              <td>
                <p class="text-xs font-medium text-slate-700 dark:text-slate-200">{formatBytes(item.storageUsedBytes)} / {storageLabel(item)}</p>
                {#if item.storageLimitBytes > 0}
                  <div class="mt-1 w-28"><ProgressBar value={percent(item.storageUsedBytes + item.storageReservedBytes, item.storageLimitBytes)} max={100} showPercent={false} tone={item.storagePercent > 90 ? 'rose' : 'brand'} /></div>
                {/if}
              </td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#if item.suspended}<span class="badge-amber">{t('admin.users.suspended')}</span>{:else}<span class="badge-green">{t('admin.users.active')}</span>{/if}
                  {#if item.mustChangePassword}<span class="badge-slate">{t('admin.users.mustChange')}</span>{/if}
                </div>
              </td>
              <td class="text-xs text-slate-500 dark:text-slate-400">{formatDate(item.createdAt)}</td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  <button type="button" class="btn-ghost btn-sm" title={t('admin.users.edit')} on:click={() => openEdit(item)}><Icon name="settings" size={15} /></button>
                  {#if item.suspended}
                    <button type="button" class="btn-ghost btn-sm" title={t('admin.users.reactivate')} on:click={() => reactivate(item)}><Icon name="check" size={15} /></button>
                  {:else}
                    <button type="button" class="btn-ghost btn-sm text-amber-600 dark:text-amber-400" title={t('admin.users.suspend')} on:click={() => suspend(item)}><Icon name="ban" size={15} /></button>
                  {/if}
                  <button type="button" class="btn-ghost btn-sm" title={t('admin.users.resetPassword')} on:click={() => resetPassword(item)}><Icon name="key" size={15} /></button>
                  <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('admin.users.delete')} on:click={() => openDelete(item)}><Icon name="trash" size={15} /></button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <Pagination {page} {limit} {total} on:change={(e) => changePage(e.detail)} />
  {/if}
</div>

<!-- Create / edit -->
<Modal open={editorOpen} title={editing ? t('admin.users.edit') : t('admin.users.createTitle')} on:close={() => (editorOpen = false)}>
  <div class="space-y-4">
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="u-username">{t('admin.users.username')}</label>
        <input id="u-username" class="input" bind:value={form.username} disabled={saving || Boolean(editing)} />
      </div>
      <div>
        <label class="label" for="u-display">{t('admin.users.displayName')}</label>
        <input id="u-display" class="input" bind:value={form.displayName} disabled={saving} />
      </div>
    </div>

    {#if !editing}
      <div>
        <label class="label" for="u-password">{t('admin.users.resetPassword')} <span class="text-slate-400">({t('common.optional')})</span></label>
        <input id="u-password" class="input" type="text" bind:value={form.password} disabled={saving} autocomplete="new-password" />
        <p class="hint">{t('admin.users.passwordPolicy')}</p>
      </div>
    {/if}

    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="u-role">{t('admin.users.role')}</label>
        <select id="u-role" class="input" bind:value={form.role} disabled={saving}>
          <option value="user">{t('profile.role.user')}</option>
          <option value="admin">{t('profile.role.admin')}</option>
        </select>
      </div>
      <div>
        <label class="label" for="u-namespace">{t('admin.users.namespace')}</label>
        <input id="u-namespace" class="input" bind:value={form.namespace} disabled={saving} placeholder="optional" />
      </div>
    </div>

    <div>
      <label class="label" for="u-storage">{t('admin.users.storageLimit')}</label>
      <div class="flex items-center gap-3">
        <input id="u-storage" class="input" type="number" min="0" step="1" bind:value={form.storageLimitGb} disabled={saving || form.unlimited} />
        <span class="text-sm text-slate-500 dark:text-slate-400">GiB</span>
      </div>
      <p class="hint">{t('admin.users.storageLimitHint')}</p>
      <div class="mt-3"><Toggle bind:checked={form.unlimited} label={t('common.unlimited')} /></div>
    </div>

    {#if domains.length > 0}
      <div>
        <span class="label">{t('admin.users.domains')}</span>
        <div class="flex flex-wrap gap-2">
          {#each domains as d (d.id)}
            <button
              type="button"
              class="rounded-xl border px-3 py-1.5 text-xs font-medium transition {form.domainIds.includes(d.id)
                ? 'border-brand-500 bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300'
                : 'border-slate-300 text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800'}"
              on:click={() => toggleDomain(d.id)}
            >
              {d.hostname}
            </button>
          {/each}
        </div>
        <p class="hint">{t('admin.users.domainsHint')}</p>
      </div>
    {/if}

    {#if editing}
      <div class="flex flex-wrap gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
        <button type="button" class="btn-secondary btn-sm" on:click={() => reset2fa(editing)}><Icon name="shield" size={15} />{t('admin.users.reset2fa')}</button>
        <button type="button" class="btn-secondary btn-sm" on:click={() => revokeSessions(editing)}><Icon name="logout" size={15} />{t('admin.users.revokeSessions')}</button>
      </div>
    {/if}

    {#if formError}
      <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300"><Icon name="error" size={16} /><span>{formError}</span></p>
    {/if}

    <div class="flex justify-end gap-2">
      <button type="button" class="btn-secondary" on:click={() => (editorOpen = false)}>{t('common.cancel')}</button>
      <button type="button" class="btn-primary" disabled={saving} on:click={saveUser}>
        {#if saving}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
        {t('common.save')}
      </button>
    </div>
  </div>
</Modal>

<!-- Temporary password -->
<Modal open={tempOpen} title={t('admin.users.tempPasswordTitle')} size="sm" on:close={() => (tempOpen = false)}>
  <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.users.tempPasswordHint')}</p>
  <div class="mt-3 flex items-center gap-2 rounded-xl bg-slate-100 px-3 py-3 dark:bg-slate-800">
    <code class="min-w-0 flex-1 break-all font-mono text-sm text-slate-800 dark:text-slate-100">{tempPassword}</code>
    <button type="button" class="btn-secondary btn-sm" on:click={copyTemp}><Icon name="copy" size={15} />{t('common.copy')}</button>
  </div>
  <div class="mt-4 flex justify-end"><button type="button" class="btn-primary" on:click={() => (tempOpen = false)}>{t('common.close')}</button></div>
</Modal>

<!-- Delete wizard -->
<Modal open={deleteOpen} title={t('admin.users.deleteTitle')} size="lg" on:close={() => (deleteOpen = false)}>
  {#if deleteUser}
    <div class="space-y-5">
      <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
        <Icon name="warning" size={16} /><span>{t('admin.users.deleteHint', { username: deleteUser.username })}</span>
      </p>

      <div>
        <label class="label" for="del-confirm">{t('admin.users.deleteConfirm')}</label>
        <input id="del-confirm" class="input" placeholder={deleteUser.username} bind:value={deleteConfirm} disabled={deleteBusy} />
      </div>

      {#if deleteShares.length > 0}
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="label" for="del-action">{t('admin.users.deleteShares')}</label>
            <select id="del-action" class="input" bind:value={deleteDefaultAction} disabled={deleteBusy}>
              <option value="trash">{t('admin.users.trashShares')}</option>
              <option value="transfer">{t('admin.users.transferTo')}</option>
            </select>
          </div>
          {#if deleteDefaultAction === 'transfer'}
            <div>
              <label class="label" for="del-target">{t('admin.users.transferTo')}</label>
              <select id="del-target" class="input" bind:value={deleteTransferTo} disabled={deleteBusy}>
                <option value={0}>—</option>
                {#each deleteOthers as other (other.id)}<option value={other.id}>{other.displayName || other.username}</option>{/each}
              </select>
            </div>
          {/if}
        </div>
        <button type="button" class="btn-secondary btn-sm" on:click={applyDefault} disabled={deleteBusy}><Icon name="check" size={15} />{t('common.all')}</button>

        <div class="mt-3 max-h-72 overflow-y-auto rounded-xl border border-slate-200 dark:border-slate-800">
          <table class="table min-w-0">
            <thead>
              <tr><th>{t('common.name')}</th><th>{t('common.actions')}</th></tr>
            </thead>
            <tbody>
              {#each deleteShares as share (share.id)}
                <tr>
                  <td class="max-w-[16rem]">
                    <p class="truncate text-sm font-medium text-slate-800 dark:text-slate-100">{share.title || share.slug}</p>
                    <p class="truncate text-xs text-slate-500 dark:text-slate-400">{share.type} · {formatBytes(share.sizeBytes)}</p>
                  </td>
                  <td>
                    {#if share.status === 'deleted' || share.status === 'expired'}
                      <span class="badge-slate">{t('admin.users.trashShares')}</span>
                    {:else}
                      <div class="flex flex-wrap items-center gap-2">
                        <select class="input w-auto py-1.5 text-xs" bind:value={deleteAssignments[share.id].action} disabled={deleteBusy}>
                          <option value="trash">{t('admin.users.trashShares')}</option>
                          <option value="transfer">{t('admin.users.transferTo')}</option>
                        </select>
                        {#if deleteAssignments[share.id].action === 'transfer'}
                          <select class="input w-auto py-1.5 text-xs" bind:value={deleteAssignments[share.id].targetUserId} disabled={deleteBusy}>
                            <option value={0}>—</option>
                            {#each deleteOthers as other (other.id)}<option value={other.id}>{other.displayName || other.username}</option>{/each}
                          </select>
                        {/if}
                      </div>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      {#if deleteError}
        <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300"><Icon name="error" size={16} /><span>{deleteError}</span></p>
      {/if}

      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={() => (deleteOpen = false)}>{t('common.cancel')}</button>
        <button type="button" class="btn-danger" disabled={!deleteReady} on:click={submitDelete}>
          {#if deleteBusy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="trash" size={16} />{/if}
          {t('admin.users.delete')}
        </button>
      </div>
    </div>
  {/if}
</Modal>
