package tests

import (
	"bytes"
	"context"
	"encoding/json"
	p "eventscope/internal/platform"
	"eventscope/migrations"
	"eventscope/projects/eventscope"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

var sharedDB *pgxpool.Pool
var sharedErr error
var dbOnce sync.Once

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedDB != nil {
		sharedDB.Close()
	}
	os.Exit(code)
}

type fixture struct {
	t      *testing.T
	app    *p.App
	db     *pgxpool.Pool
	tenant string
}

func setup(t *testing.T, name string, register func(*p.App)) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set: integration test requires PostgreSQL")
	}
	dbOnce.Do(func() { sharedDB, sharedErr = pgxpool.New(context.Background(), url) })
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	db := sharedDB
	var e error
	if e = migrations.Apply(context.Background(), db, name); e != nil {
		t.Fatal(e)
	}
	tenant := p.ID()
	tables := map[string][]string{
		"teamflow": {"task_dependencies", "task_history", "tasks"}, "bookslot": {"bookings", "resources"},
		"jobrunner": {"jobs"}, "notifyhub": {"delivery_attempts", "notifications"},
		"ordercore": {"payment_events", "order_items", "orders", "products"}, "eventscope": {"analytics_events"},
		"flagcontrol": {"flag_heads", "flags"}, "uptimewatch": {"checks", "incidents", "monitors"},
		"knowledgedesk": {"document_access", "chunks", "documents"}, "supportflow": {"ticket_audit", "support_replies", "tickets"},
	}
	t.Cleanup(func() {
		for _, table := range append(tables[name], "outbox", "idempotency", "event_receipts") {
			if _, err := db.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant=$1", tenant); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	})

	a := p.New(name, db, map[string]p.Principal{"admin-token-123456": {Tenant: tenant, User: "alice", Role: "admin"}, "editor-token-12345": {Tenant: tenant, User: "bob", Role: "editor"}, "viewer-token-12345": {Tenant: tenant, User: "reader", Role: "viewer"}, "other-token-123456": {Tenant: p.ID(), User: "mallory", Role: "admin"}})
	register(a)
	return &fixture{t, a, db, tenant}
}
func (f *fixture) call(method, path, token, key string, body any) (int, map[string]any) {
	f.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.app.Router.ServeHTTP(w, req)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return w.Code, result
}
func (f *fixture) ok(method, path string, body any) map[string]any {
	f.t.Helper()
	code, out := f.call(method, path, "admin-token-123456", "", body)
	if code != 200 {
		f.t.Fatalf("%s %s: %d %v", method, path, code, out)
	}
	return out
}
func (f *fixture) sql(q string, args ...any) {
	f.t.Helper()
	if _, e := f.db.Exec(context.Background(), q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if e := f.db.QueryRow(context.Background(), q, args...).Scan(&n); e != nil {
		f.t.Fatal(e)
	}
	return n
}
func require(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Fatal(msg)
	}
}
func TestEventScope(t *testing.T) {
	f := setup(t, "eventscope", eventscope.Register)
	now := time.Now().UTC().Truncate(time.Second)
	body := map[string]any{"events": []map[string]any{{"id": "e1", "user_id": "u1", "name": "view", "occurred_at": now}, {"id": "e2", "user_id": "u1", "name": "buy", "occurred_at": now.Add(time.Second)}, {"id": "e3", "user_id": "u2", "name": "view", "occurred_at": now}}}
	f.ok("POST", "/events", body)
	out := f.ok("POST", "/events", body)
	require(t, out["inserted"] == float64(0), "duplicate events inserted")
	funnel := f.ok("GET", "/analytics/funnel?first=view&second=buy", nil)
	require(t, funnel["entered"] == float64(2) && funnel["converted"] == float64(1), fmt.Sprint("bad funnel ", funnel))
}
