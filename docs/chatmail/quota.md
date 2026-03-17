# Storage Quota System

Madmail enforces per-user storage quotas to limit how much mailbox space each account may consume. Quotas are checked on every IMAP `GETQUOTA` / `GETQUOTAROOT` request and on every inbound SMTP delivery.

## Overview

The quota system consists of three layers:

1. **Database** — The `quotas` table stores per-user limits and the global default override.
2. **In-Memory Cache** — An RWMutex-protected hashmap that avoids hitting the database on every quota check. Populated at startup and kept in sync via write-through updates.
3. **IMAP Extension** — The IMAP `QUOTA` extension (RFC 2087) reports storage usage and limits to clients.

When a user has no per-user quota configured, the server default applies. The default can be set in `maddy.conf` or overridden at runtime via the Admin API or CLI.

## Configuration

Set the global default quota in `maddy.conf` inside the `storage.imapsql` block:

```
storage.imapsql local_mailboxes {
    # Default per-user quota (accepts human-readable sizes: 1G, 500M, etc.)
    default_quota 1G
}
```

The `default_quota` value is the config-level default. It can be overridden at runtime.

## Quota Resolution Order

When a user's quota is queried, the system resolves the limit using this priority chain:

| Priority | Source | When Used |
|----------|--------|-----------|
| 1 (highest) | Per-user entry in `quotas` table | Admin set a custom quota for this user |
| 2 | `__GLOBAL_DEFAULT__` entry in `quotas` table | Admin changed the global default via API or CLI |
| 3 (lowest) | `default_quota` in `maddy.conf` | No runtime override exists |

## In-Memory Cache

To avoid a database query on every IMAP connection or SMTP delivery, Madmail loads all quota data into memory at startup and keeps it synchronized.

### Startup Population

During server initialization, the cache is populated in a single bulk operation:

1. **Storage usage** — A single SQL query computes `SUM(bodylen)` per user from the `msgs` table, grouped by UID.
2. **Per-user limits** — All rows from the `quotas` table are loaded (excluding the `__GLOBAL_DEFAULT__` row).
3. **Default quota** — The effective default (DB override → config fallback) is determined.
4. **Cache load** — All entries are inserted into the map. Users without a custom quota receive the default.

### Concurrency Model

The cache uses `sync.RWMutex` for safe concurrent access:

- **Readers** (IMAP quota checks, delivery quota checks) acquire an `RLock` — many readers can proceed in parallel.
- **Writers** (admin quota changes, delivery used-bytes updates) acquire a full `Lock` — exclusive access.

Deadlock prevention: all lock scopes are contained within a single method. No method holds the lock while calling into external systems (database, network).

### Cache Invalidation

The cache stays consistent via **write-through** updates — every admin operation that modifies a quota writes to the database first, then updates the cache:

| Admin Action | DB Operation | Cache Operation |
|-------------|-------------|-----------------|
| Set per-user quota | `SetQuota(user, max)` | `cache.SetMax(user, max)` |
| Reset user to default | `ResetQuota(user)` | `cache.ResetMax(user)` |
| Change global default | `SetDefaultQuota(max)` | `cache.SetDefaultQuota(max)` — updates **all** entries using the old default |
| Delete account | `DeleteAccount(user)` | `cache.Invalidate(user)` |
| Successful delivery | *(implicit — body stored)* | `cache.AddUsed(user, bodyLen)` |

If the database write fails, the cache is not updated — no stale data.

## IMAP Quota (RFC 2087)

When a Delta Chat client connects, it can query the user's storage quota.

### Capability

The server advertises `QUOTA` in the IMAP capability list when the storage backend supports quotas.

### Commands

**GETQUOTA** — Returns the quota root with current usage and limit:

```
C: A1 GETQUOTA ROOT
S: * QUOTA "ROOT" (STORAGE 1024 1048576)
S: A1 OK GETQUOTA completed
```

Values are in **kilobytes** (storage usage / 1024, limit / 1024).

**GETQUOTAROOT** — Returns which quota root applies to a mailbox:

```
C: A2 GETQUOTAROOT INBOX
S: * QUOTAROOT INBOX ROOT
S: * QUOTA "ROOT" (STORAGE 1024 1048576)
S: A2 OK GETQUOTAROOT completed
```

**SETQUOTA** — Not allowed via IMAP. Use the Admin API or CLI instead.

### Data Flow

```
Client → GETQUOTA ROOT
  → imap.go getQuotaHandler.Handle()
    → Storage.GetQuota(username)
      → QuotaCache.Get(username)   ← RLock, O(1) map lookup
        → Cache HIT: return immediately (no DB query)
        → Cache MISS: fall through to DB, populate cache
  → Write RFC 2087 response: * QUOTA "ROOT" (STORAGE <usedKB> <maxKB>)
```

## SMTP Delivery Quota Check

When a message is delivered to a recipient, the quota is checked before the message body is committed:

1. **Body received** — the delivery handler calls `GetQuota(recipient)` which hits the cache.
2. **Limit check** — if `used + bodyLen > max`, the server rejects with SMTP 552 (Quota exceeded).
3. **On commit** — after the message is successfully stored, `cache.AddUsed(recipient, bodyLen)` increments the cached usage.

This ensures quota enforcement without a per-delivery database round-trip.

## Database Schema

The `quotas` table (GORM-managed, auto-migrated):

| Column | Type | Description |
|--------|------|-------------|
| `username` | string (PK) | User identifier, or `__GLOBAL_DEFAULT__` for the global default |
| `max_storage` | int64 | Maximum storage in bytes |
| `created_at` | int64 | Unix timestamp of account creation |
| `first_login_at` | int64 | Unix timestamp of first login (1 = never logged in) |
| `last_login_at` | int64 | Unix timestamp of most recent login |

## CLI Commands

All quota commands are under `maddy imap-acct quota`:

```bash
# Get a user's current usage and limit
sudo maddy imap-acct quota get user@example.org

# Set a per-user quota
sudo maddy imap-acct quota set user@example.org 2G

# Reset a user's quota to the global default
sudo maddy imap-acct quota reset user@example.org

# List all accounts with their quota info
sudo maddy imap-acct quota list

# Set the global default quota (applies to all users without a custom quota)
sudo maddy imap-acct quota set-default 5G
```

LIMIT accepts human-readable sizes: `500M`, `1G`, `2G`, etc.

## Admin API

Quota management is available through the Admin API at `POST /api/admin`:

### Get User Quota

```json
{
    "method": "GET",
    "resource": "/admin/quota",
    "headers": {"Authorization": "Bearer TOKEN"},
    "body": {"username": "user@example.org"}
}
```

Response:

```json
{
    "status": 200,
    "body": {
        "username": "user@example.org",
        "used_bytes": 52428800,
        "max_bytes": 1073741824,
        "is_default": true
    }
}
```

### Get Storage Statistics

Omit `username` to get server-wide stats:

```json
{
    "method": "GET",
    "resource": "/admin/quota",
    "headers": {"Authorization": "Bearer TOKEN"}
}
```

Response:

```json
{
    "status": 200,
    "body": {
        "total_storage_bytes": 5368709120,
        "accounts_count": 42,
        "default_quota_bytes": 1073741824
    }
}
```

### Set Per-User Quota

```json
{
    "method": "PUT",
    "resource": "/admin/quota",
    "headers": {"Authorization": "Bearer TOKEN"},
    "body": {"username": "user@example.org", "max_bytes": 2147483648}
}
```

### Set Global Default Quota

Omit `username` to set the default for all users:

```json
{
    "method": "PUT",
    "resource": "/admin/quota",
    "headers": {"Authorization": "Bearer TOKEN"},
    "body": {"max_bytes": 5368709120}
}
```

### Reset User to Default

```json
{
    "method": "DELETE",
    "resource": "/admin/quota",
    "headers": {"Authorization": "Bearer TOKEN"},
    "body": {"username": "user@example.org"}
}
```

## Admin Web Panel

The Admin panel (`/admin/`) displays quota information in the **Accounts** tab. Each account row shows current storage usage, quota limit, and whether it uses the default or a custom quota.

## Source Files

| File | Purpose |
|------|---------|
| `internal/quota/cache.go` | In-memory quota cache with RWMutex protection |
| `internal/quota/cache_test.go` | Unit tests including concurrency safety |
| `internal/storage/imapsql/imapsql.go` | Storage-level quota CRUD + cache integration |
| `internal/storage/imapsql/delivery.go` | SMTP delivery quota check + cache update on commit |
| `internal/endpoint/imap/imap.go` | IMAP QUOTA extension handlers (GETQUOTA, GETQUOTAROOT) |
| `internal/api/admin/resources/quota.go` | Admin API quota resource handler |
| `internal/cli/ctl/imapacct.go` | CLI quota subcommands |
| `framework/module/storage.go` | ManageableStorage interface (GetQuota, SetQuota, etc.) |

## Notes

- Quota values are stored and processed in **bytes** internally. The IMAP protocol reports values in **kilobytes** (divided by 1024).
- The cache does not emit any log messages containing usernames, in compliance with the [No Log Policy](nolog.md).
- Changes via the Admin API or CLI take effect **immediately** — no server restart is required.
- When the global default changes, the cache propagates the new value to all entries that were using the old default.
- The cache is resilient to initialization failures — if the bulk load fails at startup, the system falls back to per-query database lookups transparently.
