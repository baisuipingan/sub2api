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

type EventImportBatch struct{ ent.Schema }

func (EventImportBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_import_batches"}}
}

func (EventImportBatch) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (EventImportBatch) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("source_id"),
		field.String("file_name").Default("").MaxLen(255),
		field.String("file_hash").NotEmpty().MaxLen(64),
		field.Int("schema_version"),
		field.String("mode").Default("upsert").MaxLen(20),
		field.String("status").Default(domain.EventImportStatusPreviewed).MaxLen(20),
		field.Int("total_count").Default(0),
		field.Int("create_count").Default(0),
		field.Int("update_count").Default(0),
		field.Int("unchanged_count").Default(0),
		field.Int("conflict_count").Default(0),
		field.Int("error_count").Default(0),
		field.Int64("created_by"),
		field.Time("committed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EventImportBatch) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("source", EventSource.Type).Ref("import_batches").Field("source_id").Unique().Required(),
		edge.To("items", EventImportItem.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (EventImportBatch) Indexes() []ent.Index {
	return []ent.Index{index.Fields("source_id", "created_at"), index.Fields("status", "created_at"), index.Fields("file_hash")}
}
