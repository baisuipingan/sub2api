package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EventSource struct{ ent.Schema }

func (EventSource) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_sources"}}
}

func (EventSource) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (EventSource) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").NotEmpty().MaxLen(64),
		field.String("name").NotEmpty().MaxLen(100),
		field.String("kind").Default(domain.EventSourceJSON).MaxLen(20),
		field.Bool("enabled").Default(true),
		field.JSON("config", map[string]any{}).Default(map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("last_sync_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EventSource) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("records", EventSourceRecord.Type).Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("import_batches", EventImportBatch.Type).Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (EventSource) Indexes() []ent.Index {
	return []ent.Index{index.Fields("code"), index.Fields("kind", "enabled"), index.Fields("deleted_at")}
}
