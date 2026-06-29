package datasource

import (
	"strings"
	"testing"
)

func TestParse_ConnectorEnginesValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "kafka without sorting key is accepted",
			raw: `SCHEMA >
    event_id String,
    user_id String
ENGINE "Kafka"
ENGINE_KAFKA_BROKER_LIST "localhost:9092"
ENGINE_KAFKA_TOPIC_LIST "events"
ENGINE_KAFKA_GROUP_NAME "tinyraven"
ENGINE_KAFKA_FORMAT "JSONEachRow"`,
		},
		{
			name: "s3 with path + format is accepted",
			raw: `SCHEMA >
    user_id String
ENGINE "S3"
ENGINE_S3_PATH "https://bucket.s3.amazonaws.com/*.json"
ENGINE_S3_FORMAT "JSONEachRow"`,
		},
		{
			name: "postgresql with host/database/table is accepted",
			raw: `SCHEMA >
    id Int64,
    email String
ENGINE "PostgreSQL"
ENGINE_POSTGRES_HOST "localhost"
ENGINE_POSTGRES_DATABASE "app"
ENGINE_POSTGRES_TABLE "users"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse("d", tt.raw); err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
		})
	}
}

func TestParse_ConnectorEnginesMissingRequiredOpts(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "kafka missing broker and topic",
			raw: `SCHEMA >
    user_id String
ENGINE "Kafka"
ENGINE_KAFKA_GROUP_NAME "g"`,
			wantErr: "Kafka engine requires broker list",
		},
		{
			name: "kafka missing topic only",
			raw: `SCHEMA >
    user_id String
ENGINE "Kafka"
ENGINE_KAFKA_BROKER_LIST "localhost:9092"`,
			wantErr: "Kafka engine requires topic list",
		},
		{
			name: "s3 missing path",
			raw: `SCHEMA >
    user_id String
ENGINE "S3"
ENGINE_S3_FORMAT "JSONEachRow"`,
			wantErr: "S3 engine requires path",
		},
		{
			name: "postgresql missing table",
			raw: `SCHEMA >
    id Int64
ENGINE "PostgreSQL"
ENGINE_POSTGRES_HOST "localhost"
ENGINE_POSTGRES_DATABASE "app"`,
			wantErr: "PostgreSQL engine requires table",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("d", tt.raw)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// Connector engines skip the MergeTree sorting-key column-reference check, but
// SCHEMA is still required for every engine.
func TestParse_ConnectorEnginesStillRequireSchema(t *testing.T) {
	_, err := Parse("d", `ENGINE "Kafka"
ENGINE_KAFKA_BROKER_LIST "localhost:9092"
ENGINE_KAFKA_TOPIC_LIST "events"`)
	if err == nil || !strings.Contains(err.Error(), "SCHEMA is required") {
		t.Fatalf("expected SCHEMA-required error, got %v", err)
	}
}

func TestEngineFamilyClassifier(t *testing.T) {
	cases := map[string]string{
		"":           familyMergeTree,
		"MergeTree":  familyMergeTree,
		"Kafka":      familyKafka,
		"S3":         familyS3,
		"PostgreSQL": familyPostgreSQL,
		"postgres":   familyPostgreSQL,
	}
	for engine, want := range cases {
		if got := engineFamily(engine); got != want {
			t.Errorf("engineFamily(%q) = %q, want %q", engine, got, want)
		}
	}
}
