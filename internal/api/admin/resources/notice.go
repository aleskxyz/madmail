package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-message/textproto"
	"github.com/emersion/go-smtp"
	"github.com/themadorg/madmail/framework/buffer"
	"github.com/themadorg/madmail/framework/module"
)

// NoticeDeps are the dependencies needed by the notice resource handler.
type NoticeDeps struct {
	AuthDB     module.PlainUserDB
	Storage    module.ManageableStorage
	MailDomain string
}

type noticeRequest struct {
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Recipient string `json:"recipient"` // empty = broadcast to all
}

type noticeResponse struct {
	Sent   int      `json:"sent"`
	Failed int      `json:"failed"`
	Errors []string `json:"errors,omitempty"`
}

// NoticeHandler creates a handler for /admin/notice.
// POST: Send an admin notice (unencrypted email) to one or all users.
// GET: Returns basic info about the notice capability.
func NoticeHandler(deps NoticeDeps) func(string, json.RawMessage) (interface{}, int, error) {
	return func(method string, body json.RawMessage) (interface{}, int, error) {
		switch method {
		case "GET":
			// Return basic info
			users, err := deps.AuthDB.ListUsers()
			if err != nil {
				return nil, 500, fmt.Errorf("failed to list users: %v", err)
			}
			count := 0
			for _, u := range users {
				if !strings.HasPrefix(u, "__") || !strings.HasSuffix(u, "__") {
					count++
				}
			}
			return map[string]interface{}{
				"total_users": count,
				"domain":      deps.MailDomain,
			}, 200, nil

		case "POST":
			var req noticeRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, 400, fmt.Errorf("invalid request body: %v", err)
			}
			if req.Subject == "" {
				return nil, 400, fmt.Errorf("subject is required")
			}
			if req.Body == "" {
				return nil, 400, fmt.Errorf("body is required")
			}

			// Build recipient list
			var recipients []string
			if req.Recipient != "" {
				recipients = []string{req.Recipient}
			} else {
				// Broadcast to all users
				users, err := deps.AuthDB.ListUsers()
				if err != nil {
					return nil, 500, fmt.Errorf("failed to list users: %v", err)
				}
				for _, u := range users {
					if strings.HasPrefix(u, "__") && strings.HasSuffix(u, "__") {
						continue // skip internal settings keys
					}
					recipients = append(recipients, u)
				}
			}

			if len(recipients) == 0 {
				return nil, 400, fmt.Errorf("no recipients found")
			}

			// Get delivery target
			dt, ok := deps.Storage.(module.DeliveryTarget)
			if !ok {
				return nil, 500, fmt.Errorf("storage does not support delivery")
			}

			senderAddr := "admin@" + strings.Trim(deps.MailDomain, "[]")
			resp := noticeResponse{}

			// Deliver to each recipient individually
			for _, rcpt := range recipients {
				err := deliverNotice(dt, senderAddr, rcpt, req.Subject, req.Body, deps.MailDomain)
				if err != nil {
					resp.Failed++
					resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", rcpt, err))
				} else {
					resp.Sent++
				}
			}

			status := 200
			if resp.Sent == 0 && resp.Failed > 0 {
				status = 500
			}
			return resp, status, nil

		default:
			return nil, 405, fmt.Errorf("method %s not allowed", method)
		}
	}
}

// deliverNotice delivers a single admin notice email to a recipient's INBOX.
func deliverNotice(dt module.DeliveryTarget, from, to, subject, body, domain string) error {
	ctx := context.Background()

	msgID, _ := module.GenerateMsgID()
	msgMeta := &module.MsgMetadata{
		ID:       msgID,
		SMTPOpts: smtp.MailOptions{},
	}

	delivery, err := dt.Start(ctx, msgMeta, from)
	if err != nil {
		return fmt.Errorf("start delivery: %v", err)
	}
	defer func() {
		_ = delivery.Abort(ctx)
	}()

	if err := delivery.AddRcpt(ctx, to, smtp.RcptOptions{}); err != nil {
		return fmt.Errorf("add recipient: %v", err)
	}

	// Build RFC 5322 message
	now := time.Now()
	genID, _ := module.GenerateMsgID()

	header := textproto.Header{}
	header.Set("From", "Admin <"+from+">")
	header.Set("To", to)
	header.Set("Subject", subject)
	header.Set("Date", now.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	header.Set("Message-ID", "<"+genID+"@"+strings.Trim(domain, "[]")+">")
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("MIME-Version", "1.0")

	bodyBytes := []byte(body)
	// Ensure body ends with newline
	if !bytes.HasSuffix(bodyBytes, []byte("\n")) {
		bodyBytes = append(bodyBytes, '\n')
	}

	b := buffer.MemoryBuffer{Slice: bodyBytes}
	if err := delivery.Body(ctx, header, b); err != nil {
		return fmt.Errorf("deliver body: %v", err)
	}

	if err := delivery.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %v", err)
	}

	return nil
}
