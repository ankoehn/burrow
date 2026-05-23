package db

import (
	"context"
	"testing"
)

func TestMigrate0011Through0018Schema(t *testing.T) {
	x := testDB(t) // opens + migrates a temp DB; FK ON via Open()
	d := x.DB()
	ctx := context.Background()

	// Seed parent rows required by FKs.
	if _, err := d.ExecContext(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES('u1','m5@test.invalid','h','user')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := d.ExecContext(ctx,
		`INSERT INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
		   VALUES('s1','u1','web','http','m5test','open','Authorization')`); err != nil {
		t.Fatalf("seed service: %v", err)
	}

	cases := []string{
		`INSERT INTO semantic_index(service_id,exact_key_hash,prompt_fingerprint,embedding_dim,embedding_blob)
			VALUES('s1','aa','bb',768,x'00')`,
		`INSERT INTO service_upstream_credentials(service_id,slot,header_name,header_format)
			VALUES('s1','OPENAI','Authorization','Bearer {key}')`,
		`UPDATE model_aliases SET provider='ollama', priority=100 WHERE alias='nonexistent'`,
		`INSERT INTO service_custom_domains(id,service_id,hostname,cert_pem,key_pem,cert_sha256,not_before,not_after)
			VALUES('d1','s1','foo.example.com','-----BEGIN-----','-----BEGIN KEY-----','aa',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO connection_logs(id,kind,source_ip,started_at,ended_at,duration_ms,status)
			VALUES('c1','http_proxy','1.2.3.4',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,0,'closed_clean')`,
		`INSERT INTO connection_log_rollups(day,service_id,kind,sessions)
			VALUES('2026-05-20','s1','http_proxy',1)`,
		`INSERT INTO connection_log_rollup_top_ips(day,service_id,kind,ip,sessions)
			VALUES('2026-05-20','s1','http_proxy','10.0.0.1',1)`,
		`UPDATE webhooks SET payload_template='' WHERE id='nonexistent'`,
	}
	for _, q := range cases {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Errorf("query %q: %v", q, err)
		}
	}

	// 0017_retention_seed: key must exist from seed migration.
	var v string
	if err := d.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='audit.retention_days'`).Scan(&v); err != nil {
		t.Errorf("retention_seed: SELECT audit.retention_days: %v", err)
	}
}
