package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EventImportItem struct{ ent.Schema }

func (EventImportItem) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_import_items"}}
}

func (EventImportItem) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (EventImportItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("batch_id"),
		field.Int("item_index"),
		field.String("external_id").Default("").MaxLen(255),
		field.String("fingerprint").Default("").MaxLen(64),
		field.String("content_hash").Default("").MaxLen(64),
		field.String("action").NotEmpty().MaxLen(20),
		field.String("status").Default("pending").MaxLen(20),
		field.Int64("event_id").Optional().Nillable(),
		field.String("error_code").Default("").MaxLen(100),
		field.String("error_detail").Default("").MaxLen(2000),
		field.JSON("normalized_payload", map[string]any{}).Default(map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (EventImportItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("batch", EventImportBatch.Type).Ref("items").Field("batch_id").Unique().Required(),
		edge.From("event", Event.Type).Ref("import_items").Field("event_id").Unique(),
	}
}

func (EventImportItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "item_index").Unique(),
		index.Fields("batch_id", "action"),
		index.Fields("event_id"),
	}
}
