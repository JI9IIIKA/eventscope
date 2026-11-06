CREATE TABLE analytics_events(tenant text NOT NULL,id text NOT NULL,user_id text NOT NULL,name text NOT NULL,occurred_at timestamptz NOT NULL,properties jsonb NOT NULL DEFAULT '{}',exported_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant,id));
CREATE INDEX analytics_query ON analytics_events(tenant,name,occurred_at,user_id);
