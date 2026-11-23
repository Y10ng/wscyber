package webapi

import (
	"fmt"
	"sync"
	"testing"
)

// Exercise the actual automatic registration and card manual endpoint against
// the PG test database. No notifier runs until both registrations have settled.
// Ordered cases cover both lock winners; the barrier case races the writers.
func TestNotificationManualAndNewDedupe(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, order := range []string{"manual-first", "new-first", "concurrent"} {
			t.Run(fmt.Sprintf("confirm=%v/%s", confirm, order), func(t *testing.T) {
				f := newNotifyFixture(t)
				f.createAsset("a1")
				f.launchManual("a1", "t1")
				fid, rev := f.addFinding("t1", "合成通知候选", notifyContent(), evidenceAt("GET", "https://api.example.test/items/42"))
				verification := f.verify("t1", fid, rev, "supported", notifyFlow("GET", "https://api.example.test/items/42", "", `{"id":42}`))
				key := vulnOf(f.vulns("a1", ""), fid).ID
				if len(f.notifications()) != 0 {
					t.Fatal("disabled notification unexpectedly registered")
				}
				f.enable(confirm, "")
				var autoErr error
				var status int
				var value map[string]any
				var raw string
				automatic := func() {
					autoErr = f.capability.registerVerificationNotification(t.Context(), verification.VerificationID)
				}
				manual := func() { status, value, raw = f.cardNotify(key, "") }
				switch order {
				case "manual-first":
					manual()
					automatic()
				case "new-first":
					automatic()
					manual()
				default:
					start := make(chan struct{})
					var wg sync.WaitGroup
					for _, run := range []func(){automatic, manual} {
						wg.Add(1)
						go func() { defer wg.Done(); <-start; run() }()
					}
					close(start)
					wg.Wait()
				}
				if autoErr != nil {
					t.Fatal(autoErr)
				}
				rows := f.notifications()
				if len(rows) != 1 {
					t.Fatalf("duplicate registration: %+v; manual=%d %s", rows, status, raw)
				}
				row := rows[0]
				if row.DedupeKey != key || row.VerificationID != verification.VerificationID {
					t.Fatalf("wrong notification: %+v", row)
				}
				switch row.Kind {
				case NotifyKindManual:
					if status != 201 || row.Status != NotifyQueued || order == "new-first" {
						t.Fatalf("manual winner: %+v, response=%d %s", row, status, raw)
					}
				case NotifyKindNew:
					if status != 409 || value["code"] != "in_flight" || value["notification_id"] != row.ID || order == "manual-first" {
						t.Fatalf("new winner: %+v, response=%d %s", row, status, raw)
					}
				default:
					t.Fatalf("unexpected kind: %+v", row)
				}
				if row.Status == NotifyPendingConfirm {
					if code, _, body := f.do("POST", "/api/webapi/notifications/"+row.ID+"/confirm", ""); code != 200 {
						t.Fatalf("confirm=%d %s", code, body)
					}
				}
				f.deliver()
				f.deliver() // no second delivery even when the queue is drained again
				markdown, images := 0, 0
				for _, body := range f.wecom.received() {
					switch body["msgtype"] {
					case "markdown":
						markdown++
					case "image":
						images++
					}
				}
				if markdown != 1 || images != 1 || f.notificationByID(row.ID).Status != NotifySent {
					t.Fatalf("delivery count markdown=%d image=%d row=%+v", markdown, images, f.notificationByID(row.ID))
				}
				// A late/replayed automatic registration still cannot create new after sent.
				automatic()
				if autoErr != nil || len(f.notifications()) != 1 {
					t.Fatalf("late automatic registration: %v %+v", autoErr, f.notifications())
				}
			})
		}
	}
}
