package app

import "time"

// User is the internal representation of an account.
type User struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	DisplayName         string `json:"displayName"`
	PasswordHash        string `json:"-"`
	Role                string `json:"role"`
	MustChangePassword  bool   `json:"mustChangePassword"`
	Suspended           bool   `json:"suspended"`
	SuspendedAt         int64  `json:"suspendedAt,omitempty"`
	SuspendedReason     string `json:"suspendedReason,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	StorageLimitBytes   int64  `json:"storageLimitBytes"`
	StorageUsedBytes    int64  `json:"storageUsedBytes"`
	StorageReservedBytes int64 `json:"storageReservedBytes"`
	AvatarPath          string `json:"-"`
	TOTPEnabled         bool   `json:"totpEnabled"`
	TOTPLastStep        int64  `json:"-"`
	Language            string `json:"language"`
	Theme               string `json:"theme"`
	NotificationPrefs   string `json:"-"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
	LastLoginAt         int64  `json:"lastLoginAt,omitempty"`
	FailedLoginCount    int    `json:"-"`
	LockedUntil         int64  `json:"-"`
}

// HasAvatar is used by JSON DTOs.
func (u *User) HasAvatar() bool { return u.AvatarPath != "" }

// Session is an authenticated login session.
type Session struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Realm      string `json:"realm"`
	CSRFToken  string `json:"csrfToken"`
	Remember   bool   `json:"remember"`
	UserAgent  string `json:"userAgent"`
	Browser    string `json:"browser"`
	OS         string `json:"os"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	ExpiresAt  int64  `json:"expiresAt"`
	Current    bool   `json:"current"`
}

// Domain is a public hostname usable for share links.
type Domain struct {
	ID                int64  `json:"id"`
	Hostname          string `json:"hostname"`
	Enabled           bool   `json:"enabled"`
	Verified          bool   `json:"verified"`
	VerificationToken string `json:"verificationToken"`
	IsDefault         bool   `json:"isDefault"`
	TLSStatus         string `json:"tlsStatus"`
	LastCheckedAt     int64  `json:"lastCheckedAt"`
	LastCheckOK       *bool  `json:"lastCheckOk,omitempty"`
	LastCheckError    string `json:"lastCheckError,omitempty"`
	CreatedAt         int64  `json:"createdAt"`
	UserCount         int64  `json:"userCount,omitempty"`
	ShareCount        int64  `json:"shareCount,omitempty"`

	// Flexible DNS / network binding. Empty values inherit the global setting.
	DNSProvider   string `json:"dnsProvider"`
	DNSRecordType string `json:"dnsRecordType"`
	DNSTarget     string `json:"dnsTarget"`
	DNSZoneID     string `json:"dnsZoneId"`
	DNSRecordID   string `json:"dnsRecordId"`
	DNSStatus     string `json:"dnsStatus"`
	DNSCheckedAt  int64  `json:"dnsCheckedAt"`
	DNSOK         *bool  `json:"dnsOk,omitempty"`
	DNSError      string `json:"dnsError,omitempty"`
	NetworkMode   string `json:"networkMode"`
	ProxyKind     string `json:"proxyKind"`
}

// Share is the central share record.
type Share struct {
	ID               string `json:"id"`
	OwnerID          int64  `json:"ownerId"`
	OwnerUsername    string `json:"ownerUsername"`
	Type             string `json:"type"`
	DomainID         int64  `json:"domainId"`
	DomainHostname   string `json:"domainHostname"`
	DomainEnabled    bool   `json:"domainEnabled"`
	DomainVerified   bool   `json:"domainVerified"`
	Namespace        string `json:"namespace"`
	Slug             string `json:"slug"`
	TokenHash        string `json:"-"`
	Title            string `json:"title,omitempty"`
	Description      string `json:"description,omitempty"`
	LinkURL          string `json:"linkUrl,omitempty"`
	TextContent      string `json:"textContent,omitempty"`
	PasswordHash     string `json:"-"`
	IsE2EE           bool   `json:"isE2ee"`
	E2EEMode         string `json:"e2eeMode,omitempty"`
	E2EEManifest     []byte `json:"-"`
	E2EEKeyWrap      string `json:"-"`
	E2EERecoveryWrap string `json:"-"`
	Status           string `json:"status"`
	AdminLocked      bool   `json:"adminLocked"`
	AdminLockReason  string `json:"adminLockReason,omitempty"`
	SuspendedBlock   bool   `json:"suspendedBlock"`
	ExpiresAt        int64  `json:"expiresAt,omitempty"`
	DownloadLimit    int64  `json:"downloadLimit"`
	DownloadCount    int64  `json:"downloadCount"`
	ViewCount        int64  `json:"viewCount"`
	BytesServed      int64  `json:"bytesServed"`
	SizeBytes        int64  `json:"sizeBytes"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
	DeletedAt        int64  `json:"deletedAt,omitempty"`
	PurgeAfter       int64  `json:"purgeAfter,omitempty"`
	LastAccessedAt   int64  `json:"lastAccessedAt,omitempty"`
	Version          int64  `json:"version"`
	Files            []ShareFile `json:"files,omitempty"`
	PasswordSet      bool   `json:"passwordSet"`
	EffectiveStatus  string `json:"effectiveStatus,omitempty"`
	Quarantine       bool   `json:"quarantine"`
	OwnerSuspended   bool   `json:"ownerSuspended"`
}

// ShareFile is a file attached to a share.
type ShareFile struct {
	ID               string `json:"id"`
	ShareID          string `json:"-"`
	Position         int    `json:"position"`
	Name             string `json:"name"`
	Mime             string `json:"mime"`
	SizeBytes        int64  `json:"sizeBytes"`
	EncryptedSizeBytes int64 `json:"encryptedSizeBytes,omitempty"`
	StoragePath      string `json:"-"`
	SHA256           string `json:"sha256,omitempty"`
	ScanStatus       string `json:"scanStatus"`
	ScanMessage      string `json:"scanMessage,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
	PreviewKind      string `json:"previewKind,omitempty"`
}

// Upload is a staged upload.
type Upload struct {
	ID          string `json:"id"`
	UserID      int64  `json:"-"`
	Filename    string `json:"filename"`
	Mime        string `json:"mime"`
	SizeBytes   int64  `json:"sizeBytes"`
	ChunkSize   int64  `json:"chunkSize"`
	TotalChunks int    `json:"totalChunks"`
	Encrypted   bool   `json:"encrypted"`
	Status      string `json:"status"`
	StoragePath string `json:"-"`
	SHA256      string `json:"-"`
	ScanStatus  string `json:"scanStatus"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

// Notification is an in-app notification.
type Notification struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Data      string `json:"data"`
	ReadAt    int64  `json:"readAt,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Activity is a user-visible event.
type Activity struct {
	ID        int64  `json:"id"`
	ShareID   string `json:"shareId,omitempty"`
	Type      string `json:"type"`
	Meta      string `json:"meta"`
	CreatedAt int64  `json:"createdAt"`
}

// LoginResult is returned by the login endpoint.
type LoginResult struct {
	OK           bool   `json:"ok"`
	TOTPRequired bool   `json:"totpRequired"`
	Challenge    string `json:"challenge,omitempty"`
	User         map[string]any `json:"user,omitempty"`
	CSRF         string `json:"csrf,omitempty"`
}

// APIError is the uniform error envelope.
type APIError struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// TimeNow is overridable in tests.
var TimeNow = func() time.Time { return time.Now() }
