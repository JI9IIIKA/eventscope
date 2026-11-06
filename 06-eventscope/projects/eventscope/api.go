package eventscope

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	p "eventscope/internal/platform"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"os"
	"time"
)

type Event struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id"`
	Name       string         `json:"name"`
	OccurredAt time.Time      `json:"occurred_at"`
	Properties map[string]any `json:"properties"`
}

func Register(a *p.App) {
	a.Handle("POST", "/events", func(r *p.Request) (any, error) {
		var b struct {
			Events []Event `json:"events"`
		}
		if e := r.Decode(&b); e != nil {
			return nil, e
		}
		if len(b.Events) == 0 || len(b.Events) > 500 {
			return nil, p.Bad("1..500 events required")
		}
		inserted := 0
		for _, event := range b.Events {
			if event.ID == "" || event.UserID == "" || event.Name == "" || event.OccurredAt.IsZero() || event.OccurredAt.After(time.Now().Add(5*time.Minute)) {
				return nil, p.Bad("valid id, user_id, name and occurred_at required")
			}
			props, _ := json.Marshal(event.Properties)
			tag, e := r.Tx.Exec(r.Ctx, `INSERT INTO analytics_events(tenant,id,user_id,name,occurred_at,properties) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, r.Tenant, event.ID, event.UserID, event.Name, event.OccurredAt, string(props))
			if e != nil {
				return nil, e
			}
			if tag.RowsAffected() == 1 {
				inserted++
				if e = r.Event("analytics.event", event); e != nil {
					return nil, e
				}
			} else {
				var same bool
				if e = r.Tx.QueryRow(r.Ctx, `SELECT user_id=$3 AND name=$4 AND occurred_at=$5 AND properties=$6::jsonb FROM analytics_events WHERE tenant=$1 AND id=$2`, r.Tenant, event.ID, event.UserID, event.Name, event.OccurredAt, string(props)).Scan(&same); e != nil {
					return nil, e
				}
				if !same {
					return nil, p.Conflict("event id reused with different content")
				}
			}
		}
		return map[string]int{"inserted": inserted, "duplicates": len(b.Events) - inserted}, nil
	})
	a.Handle("GET", "/analytics/daily", func(r *p.Request) (any, error) {
		return r.All(`SELECT (occurred_at AT TIME ZONE 'UTC')::date AS day,name,count(*) AS events,count(DISTINCT user_id) AS users FROM analytics_events WHERE tenant=$1 AND occurred_at>=now()-interval '30 days' GROUP BY day,name ORDER BY day,name`, r.Tenant)
	})
	a.Handle("GET", "/analytics/funnel", func(r *p.Request) (any, error) {
		first, second := r.HTTP.URL.Query().Get("first"), r.HTTP.URL.Query().Get("second")
		if first == "" || second == "" || first == second {
			return nil, p.Bad("distinct first and second event names required")
		}
		return r.One(`WITH starts AS (SELECT user_id,min(occurred_at) AS at FROM analytics_events WHERE tenant=$1 AND name=$2 GROUP BY user_id), converted AS(SELECT s.user_id FROM starts s WHERE EXISTS(SELECT 1 FROM analytics_events e WHERE e.tenant=$1 AND e.user_id=s.user_id AND e.name=$3 AND e.occurred_at>s.at)) SELECT (SELECT count(*) FROM starts) AS entered,(SELECT count(*) FROM converted) AS converted`, r.Tenant, first, second)
	})
	a.Handle("GET", "/analytics/retention", func(r *p.Request) (any, error) {
		return r.All(`WITH cohorts AS(SELECT user_id,min((occurred_at AT TIME ZONE 'UTC')::date) AS cohort FROM analytics_events WHERE tenant=$1 GROUP BY user_id) SELECT c.cohort,((e.occurred_at AT TIME ZONE 'UTC')::date-c.cohort) AS day,count(DISTINCT e.user_id) AS users FROM cohorts c JOIN analytics_events e ON e.tenant=$1 AND e.user_id=c.user_id GROUP BY c.cohort,day ORDER BY c.cohort,day LIMIT 500`, r.Tenant)
	})
	a.Workers = append(a.Workers, Export)
}

// ReplacingMergeTree + FINAL yields deduplicated reads after uncertain export outcomes.
func Export(ctx context.Context, db *pgxpool.Pool) error {
	endpoint := os.Getenv("CLICKHOUSE_URL")
	if endpoint == "" {
		return nil
	}
	ctx, c := context.WithTimeout(ctx, 8*time.Second)
	defer c()
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var tenant, id, user, name string
	var at time.Time
	var properties []byte
	e = tx.QueryRow(ctx, `SELECT tenant,id,user_id,name,occurred_at,properties FROM analytics_events WHERE exported_at IS NULL ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&tenant, &id, &user, &name, &at, &properties)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	row, _ := json.Marshal(map[string]any{"tenant": tenant, "id": id, "user_id": user, "name": name, "occurred_at": at.UTC().Format("2006-01-02 15:04:05.000"), "properties": string(properties)})
	body := append([]byte("INSERT INTO events FORMAT JSONEachRow\n"), row...)
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.SetBasicAuth(p.Env("CLICKHOUSE_USER", "portfolio"), os.Getenv("CLICKHOUSE_PASSWORD"))
	resp, e := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("clickhouse HTTP %d", resp.StatusCode)
	}
	if _, e = tx.Exec(ctx, `UPDATE analytics_events SET exported_at=now() WHERE tenant=$1 AND id=$2`, tenant, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
