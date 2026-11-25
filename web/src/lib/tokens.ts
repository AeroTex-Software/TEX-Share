export interface OwnerShareRecord {
  token?: string;
  key?: string;
  title?: string;
  type?: string;
  e2ee?: boolean;
  e2eeMode?: string;
  updatedAt?: number;
}

const STORAGE_KEY = 'texshare_owner_shares_v1';

type Store = Record<string, OwnerShareRecord>;

function read(): Store {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as Store) : {};
  } catch {
    return {};
  }
}

function write(store: Store): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch {
    // Storage can be unavailable in private modes; sharing still works via
    // rotation, so never break the UI.
  }
}

export function getOwnerShare(id: string): OwnerShareRecord {
  return read()[id] || {};
}

export function setOwnerShare(id: string, patch: OwnerShareRecord): OwnerShareRecord {
  const store = read();
  store[id] = { ...(store[id] || {}), ...patch, updatedAt: Date.now() };
  write(store);
  return store[id];
}

export function removeOwnerShare(id: string): void {
  const store = read();
  delete store[id];
  write(store);
}

export function allOwnerShares(): Store {
  return read();
}
