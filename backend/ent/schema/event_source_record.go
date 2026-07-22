package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EventSourceRecord struct{ ent.Schema }

func (EventSourceRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_source_records"}}
}

func (EventSourceRecord) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (EventSourceRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("source_id"),
		field.Int64("event_id").Optional().Nillable(),
		field.String("external_id").Default("").MaxLen(255),
		field.String("source_url").Default("").MaxLen(2048),
		field.String("fingerprint").NotEmpty().MaxLen(64),
		field.String("content_hash").NotEmpty().MaxLen(64),
		field.String("state").Default("active").MaxLen(20),
		field.JSON("raw_payload", map[string]any{}).Default(map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("normalized_payload", map[string]any{}).Default(map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("source_updated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("first_seen_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_seen_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EventSourceRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("source", EventSource.Type).Ref("records").Field("source_id").Unique().Required(),
		edge.From("event", Event.Type).Ref("source_records").Field("event_id").Unique(),
	}
}

func (EventSourceRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "external_id"),
		index.Fields("source_id", "fingerprint").Unique(),
		index.Fields("fingerprint"),
		index.Fields("event_id"),
		index.Fields("last_seen_at"),
	}
}
