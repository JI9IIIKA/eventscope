package main

import (
	"context"
	"eventscope/internal/platform"
	"eventscope/migrations"
	"eventscope/projects/eventscope"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	platform.Serve("eventscope", eventscope.Register, func(ctx context.Context, pool *pgxpool.Pool) error { return migrations.Apply(ctx, pool, "eventscope") })
}
