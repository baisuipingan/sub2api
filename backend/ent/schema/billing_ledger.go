package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BillingLedger is the append-only wallet ledger. SQL migration 246 creates
// the production table; the Ent schema keeps tests and future queries aligned.
type BillingLedger struct {
	ent.Schema
}

func (BillingLedger) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "billing_ledger"}}
}

func (BillingLedger) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("delta_credits"),
		field.Int64("balance_after"),
		field.String("bucket_type").
			MaxLen(32).
			Default("wallet"),
		field.Int64("bucket_id"),
		field.String("reference_type").
			MaxLen(32),
		field.String("reference_id").
			MaxLen(128),
		field.String("request_id").
			MaxLen(128).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]any{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BillingLedger) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("bucket_type", "bucket_id", "reference_type", "reference_id").Unique(),
		index.Fields("user_id", "created_at"),
		index.Fields("request_id"),
	}
}
