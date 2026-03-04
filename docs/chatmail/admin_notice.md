# Admin Notice

## Overview
Admin notices allow the server administrator to send unencrypted email messages
directly to user inboxes. Messages are delivered via the IMAP delivery pipeline —
they appear as regular emails in the user's INBOX.

## API

### GET `/admin/notice`
Returns the total user count and mail domain.

**Response:**
```json
{
  "total_users": 560,
  "domain": "example.com"
}
```

### POST `/admin/notice`
Send a notice to one or all users.

**Request:**
```json
{
  "subject": "Server Maintenance",
  "body": "The server will be down for maintenance on Saturday.",
  "recipient": ""
}
```

| Field       | Type   | Description                                      |
|-------------|--------|--------------------------------------------------|
| `subject`   | string | Email subject line (required)                    |
| `body`      | string | Plain text message body (required)               |
| `recipient` | string | Target email address. Empty = broadcast to all.  |

**Response:**
```json
{
  "sent": 559,
  "failed": 1,
  "errors": ["broken@example.com: add recipient: User does not exist"]
}
```

## CLI Usage
```bash
# Send to a specific user
curl -X POST https://your-server/api/admin \
  -H 'Content-Type: application/json' \
  -d '{
    "method": "POST",
    "resource": "/admin/notice",
    "headers": {"Authorization": "Bearer YOUR_TOKEN"},
    "body": {
      "subject": "Test notice",
      "body": "Hello from admin",
      "recipient": "user@example.com"
    }
  }'

# Broadcast to all users (recipient = "")
curl -X POST https://your-server/api/admin \
  -H 'Content-Type: application/json' \
  -d '{
    "method": "POST",
    "resource": "/admin/notice",
    "headers": {"Authorization": "Bearer YOUR_TOKEN"},
    "body": {
      "subject": "Server announcement",
      "body": "Important update for all users",
      "recipient": ""
    }
  }'
```

## Web Dashboard
The admin-web dashboard includes a **Notice** tab where administrators can:
- Toggle between sending to all users or a specific user
- Enter a subject and message body
- Confirm before broadcasting to all users
- See delivery results (sent/failed counts and errors)

## Privacy
- Messages are plain text (unencrypted) — they are admin announcements, not private communications
- No logs are generated for the delivery when logging is disabled (respects No Log policy)
- The sender address is `admin@<domain>`
