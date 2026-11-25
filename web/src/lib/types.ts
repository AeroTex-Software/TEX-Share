export type Role = 'admin' | 'user';
export type Theme = 'system' | 'light' | 'dark';
export type Language = 'auto' | 'de' | 'en';
export type Realm = 'public' | 'admin';

export interface User {
  id: number;
  username: string;
  displayName: string;
  role: Role;
  mustChangePassword: boolean;
  suspended: boolean;
  suspendedReason?: string;
  namespace: string;
  storageLimitBytes: number;
  storageUsedBytes: number;
  storageReservedBytes: number;
  totpEnabled: boolean;
  language: Language;
  theme: Theme;
  notificationPrefs: Record<string, boolean>;
  avatarUrl: string;
  createdAt: number;
  lastLoginAt: number;
  shareCount?: number;
  domains?: number[];
  storagePercent?: number;
  totalShares?: number;
}

export interface ShareFile {
  id: string;
  position: number;
  name: string;
  mime: string;
  sizeBytes: number;
  encryptedSizeBytes?: number;
  sha256?: string;
  scanStatus: string;
  scanMessage?: string;
  createdAt: number;
  previewKind?: string;
}

export interface Share {
  id: string;
  ownerId: number;
  ownerUsername: string;
  type: string;
  domainId: number;
  domainHostname: string;
  namespace: string;
  slug: string;
  title: string;
  description: string;
  linkUrl: string;
  textContent: string;
  isE2ee: boolean;
  e2eeMode: 'key' | 'password' | '';
  status: string;
  effectiveStatus: string;
  adminLocked: boolean;
  adminLockReason?: string;
  suspendedBlock: boolean;
  expiresAt: number;
  downloadLimit: number;
  downloadCount: number;
  viewCount: number;
  bytesServed: number;
  sizeBytes: number;
  createdAt: number;
  updatedAt: number;
  deletedAt: number;
  purgeAfter: number;
  lastAccessedAt: number;
  passwordSet: boolean;
  quarantine: boolean;
  files: ShareFile[];
  urlPath: string;
  domainVerified: boolean;
  ownerSuspended: boolean;
  e2eeManifest?: string;
  e2eeKeyWrap?: string;
  e2eeRecoveryWrap?: string;
  token?: string;
  sharePath?: string;
  daily?: DailyStat[];
  encryptedOnly?: boolean;
  notice?: string;
}

export interface DailyStat {
  day: string;
  views: number;
  downloads: number;
  bytes: number;
}

export interface Domain {
  id: number;
  hostname: string;
  enabled: boolean;
  verified: boolean;
  verificationToken: string;
  isDefault: boolean;
  tlsStatus: string;
  lastCheckedAt: number;
  lastCheckOk?: boolean;
  lastCheckError?: string;
  createdAt: number;
  userCount?: number;
  shareCount?: number;
}

export interface SessionInfo {
  id: number;
  realm: string;
  csrfToken?: string;
  remember: boolean;
  userAgent: string;
  browser: string;
  os: string;
  createdAt: number;
  lastSeenAt: number;
  expiresAt: number;
  current: boolean;
}

export interface Notification {
  id: number;
  type: string;
  data: Record<string, unknown>;
  readAt: number;
  createdAt: number;
}

export interface Activity {
  id: number;
  shareId: string;
  type: string;
  meta: Record<string, unknown>;
  createdAt: number;
}

export interface AuditEntry {
  id: number;
  ts: number;
  actorId: number;
  actor: string;
  action: string;
  objectType: string;
  objectId: string;
  result: string;
  details: unknown;
  prevHash: string;
  entryHash: string;
}

export interface Backup {
  id: string;
  filename: string;
  sizeBytes: number;
  kind: string;
  encrypted: boolean;
  createdAt: number;
  status: string;
  error?: string;
}

export interface UploadInit {
  id: string;
  mode: 'direct' | 'chunked';
  chunkSize: number;
  totalChunks: number;
  size: number;
  filename: string;
  expiresAt: number;
}

export interface UploadedFile {
  id: string;
  filename: string;
  mime: string;
  size: number;
  encrypted: boolean;
  scanStatus: string;
  salt?: string;
}

export interface PublicSharePayload {
  id: string;
  type: string;
  isE2ee: boolean;
  e2eeMode: 'key' | 'password' | '';
  available: boolean;
  state: string;
  requiresPassword: boolean;
  accessGranted: boolean;
  createdAt: number;
  expiresAt: number;
  downloadCount: number;
  downloadLimit: number;
  downloadsRemaining?: number;
  viewCount: number;
  domainHostname: string;
  slug: string;
  namespace: string;
  title?: string;
  description?: string;
  textContent?: string;
  linkUrl?: string;
  e2eeManifest?: string;
  e2eeKeyWrap?: string;
  e2eeRecoveryWrap?: string;
  files?: ShareFile[];
  downloadAllowed?: boolean;
  zipAvailable?: boolean;
}

export interface PublicMeta {
  appName: string;
  version: string;
  setupNeeded: boolean;
  maintenance: boolean;
  branding: { appName: string; logo: boolean; favicon: boolean; logoUrl: string; faviconUrl: string };
  domain: string;
  languages: string[];
  allow2fa: boolean;
  registration: boolean;
}

export interface SystemInfo {
  version: string;
  commit: string;
  go: string;
  os: string;
  arch: string;
  cpus: number;
  startedAt: number;
  uptimeSeconds: number;
  heapBytes: number;
  sysBytes: number;
  goroutines: number;
  dbSizeBytes: number;
  errors: number;
  publicUrl?: string;
  adminUrl?: string;
}
