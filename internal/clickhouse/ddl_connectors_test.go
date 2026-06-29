package clickhouse

import (
	"context"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

func TestEngineFamily(t *testing.T) {
	cases := map[string]string{
		"":                        familyMergeTree, // empty -> MergeTree default path
		"MergeTree":               familyMergeTree,
		"ReplacingMergeTree":      familyMergeTree,
		"ReplacingMergeTree(ver)": familyMergeTree,
		"  mergetree  ":           familyMergeTree,
		"Kafka":                   familyKafka,
		"Kafka()":                 familyKafka,
		"S3":                      familyS3,
		"PostgreSQL":              familyPostgreSQL,
		"postgres":                familyPostgreSQL,
		"Distributed":             familyMergeTree, // unknown -> MergeTree path
	}
	for engine, want := range cases {
		if got := engineFamily(engine); got != want {
			t.Errorf("engineFamily(%q) = %q, want %q", engine, got, want)
		}
	}
}

func TestBuildKafkaTableDDL(t *testing.T) {
	ds := &model.Datasource{
		Name:   "kafka_events",
		Schema: []model.Column{{Name: "event_id", Type: "String"}, {Name: "user_id", Type: "String"}},
		Engine: "Kafka",
		EngineOpts: map[string]string{
			"ENGINE_KAFKA_BROKER_LIST":   "localhost:9092",
			"ENGINE_KAFKA_TOPIC_LIST":    "events",
			"ENGINE_KAFKA_GROUP_NAME":    "tinyraven",
			"ENGINE_KAFKA_FORMAT":        "JSONEachRow",
			"ENGINE_KAFKA_NUM_CONSUMERS": "2",         // numeric -> bare, sorted after known keys
			"ENGINE_KAFKA_POLL_TIMEOUT":  "ms-string", // non-numeric -> quoted
		},
	}
	got := buildCreateTable(ds.Name, ds)

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS `kafka_events`",
		"`event_id` String",
		"ENGINE = Kafka()",
		"SETTINGS kafka_broker_list = 'localhost:9092'",
		"kafka_topic_list = 'events'",
		"kafka_group_name = 'tinyraven'",
		"kafka_format = 'JSONEachRow'",
		"kafka_num_consumers = 2",          // numeric, no quotes
		"kafka_poll_timeout = 'ms-string'", // non-numeric, quoted
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Kafka DDL missing %q; got:\n%s", want, got)
		}
	}
	// Connector engines never emit MergeTree clauses.
	for _, bad := range []string{"ORDER BY", "PARTITION BY", "TTL"} {
		if strings.Contains(got, bad) {
			t.Errorf("Kafka DDL must not contain %q; got:\n%s", bad, got)
		}
	}
	// Well-known settings come before the extra (sorted) ones.
	if i, j := strings.Index(got, "kafka_format"), strings.Index(got, "kafka_num_consumers"); i > j {
		t.Errorf("well-known kafka settings should precede extras; got:\n%s", got)
	}
}

func TestBuildS3TableDDL(t *testing.T) {
	base := &model.Datasource{
		Name:   "s3_import",
		Schema: []model.Column{{Name: "user_id", Type: "String"}},
		Engine: "S3",
		EngineOpts: map[string]string{
			"ENGINE_S3_PATH":   "https://bucket.s3.amazonaws.com/*.json.gz",
			"ENGINE_S3_FORMAT": "JSONEachRow",
		},
	}
	got := buildCreateTable(base.Name, base)
	if want := "ENGINE = S3('https://bucket.s3.amazonaws.com/*.json.gz', 'JSONEachRow')"; !strings.Contains(got, want) {
		t.Errorf("S3 DDL = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "ORDER BY") {
		t.Errorf("S3 DDL must not contain ORDER BY; got:\n%s", got)
	}

	// With static credentials + compression: positional order path, key, secret,
	// format, compression.
	withCreds := &model.Datasource{
		Name:   "s3_import",
		Schema: base.Schema,
		Engine: "S3",
		EngineOpts: map[string]string{
			"ENGINE_S3_PATH":                  "s3://b/x.csv",
			"ENGINE_S3_AWS_ACCESS_KEY_ID":     "AKIA",
			"ENGINE_S3_AWS_SECRET_ACCESS_KEY": "secret",
			"ENGINE_S3_FORMAT":                "CSV",
			"ENGINE_S3_COMPRESSION":           "gzip",
		},
	}
	got = buildCreateTable(withCreds.Name, withCreds)
	if want := "ENGINE = S3('s3://b/x.csv', 'AKIA', 'secret', 'CSV', 'gzip')"; !strings.Contains(got, want) {
		t.Errorf("S3 creds DDL = %q, want it to contain %q", got, want)
	}
}

func TestBuildPostgreSQLTableDDL(t *testing.T) {
	ds := &model.Datasource{
		Name:   "postgres_users",
		Schema: []model.Column{{Name: "id", Type: "Int64"}, {Name: "email", Type: "String"}},
		Engine: "PostgreSQL",
		EngineOpts: map[string]string{
			"ENGINE_POSTGRES_HOST":     "db.internal",
			"ENGINE_POSTGRES_DATABASE": "app",
			"ENGINE_POSTGRES_TABLE":    "users",
			"ENGINE_POSTGRES_USER":     "readonly",
			"ENGINE_POSTGRES_PASSWORD": "pw",
		},
	}
	got := buildCreateTable(ds.Name, ds)
	// Port defaults to 5432 when unset.
	if want := "ENGINE = PostgreSQL('db.internal:5432', 'app', 'users', 'readonly', 'pw')"; !strings.Contains(got, want) {
		t.Errorf("PostgreSQL DDL = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "ORDER BY") {
		t.Errorf("PostgreSQL DDL must not contain ORDER BY; got:\n%s", got)
	}

	// ENGINE_PG_* spelling and explicit port are accepted too.
	pg := &model.Datasource{
		Name:   "postgres_users",
		Schema: ds.Schema,
		Engine: "postgres",
		EngineOpts: map[string]string{
			"ENGINE_PG_HOST":     "h",
			"ENGINE_PG_PORT":     "6543",
			"ENGINE_PG_DATABASE": "d",
			"ENGINE_PG_TABLE":    "t",
			"ENGINE_PG_USER":     "u",
		},
	}
	got = buildCreateTable(pg.Name, pg)
	if want := "ENGINE = PostgreSQL('h:6543', 'd', 't', 'u', '')"; !strings.Contains(got, want) {
		t.Errorf("PG-spelling DDL = %q, want it to contain %q", got, want)
	}
}

func TestSQLStringLitEscapesQuotes(t *testing.T) {
	if got := sqlStringLit("a'b"); got != "'a''b'" {
		t.Errorf("sqlStringLit = %q, want escaped single quote", got)
	}
}

// EnsureTable must skip the quarantine sibling for connector engines (ADR 0019);
// quarantine is the Gatherer's HTTP-ingest landing zone only (ADR 0018).
func TestEnsureTableSkipsQuarantineForConnectors(t *testing.T) {
	c, cap := newCaptureClient(t)
	kafka := &model.Datasource{
		Name:   "kafka_events",
		Schema: []model.Column{{Name: "user_id", Type: "String"}},
		Engine: "Kafka",
		EngineOpts: map[string]string{
			"ENGINE_KAFKA_BROKER_LIST": "localhost:9092",
			"ENGINE_KAFKA_TOPIC_LIST":  "events",
		},
	}
	if err := c.EnsureTable(context.Background(), kafka); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	// capture only retains the LAST request body; for a connector that must be
	// the Kafka CREATE TABLE, never a quarantine table.
	if strings.Contains(cap.body, "_quarantine") {
		t.Errorf("connector EnsureTable should not create a quarantine table; last DDL:\n%s", cap.body)
	}
	if !strings.Contains(cap.body, "ENGINE = Kafka()") {
		t.Errorf("last DDL should be the Kafka CREATE TABLE; got:\n%s", cap.body)
	}

	// A MergeTree datasource still gets its quarantine sibling (last request).
	mt := &model.Datasource{
		Name:       "events",
		Schema:     []model.Column{{Name: "user_id", Type: "String"}},
		Engine:     "MergeTree",
		EngineOpts: map[string]string{},
	}
	if err := c.EnsureTable(context.Background(), mt); err != nil {
		t.Fatalf("EnsureTable mergetree: %v", err)
	}
	if !strings.Contains(cap.body, "`events_quarantine`") {
		t.Errorf("MergeTree EnsureTable should create quarantine; last DDL:\n%s", cap.body)
	}
}
