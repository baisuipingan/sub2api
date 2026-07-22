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

type Event struct{ ent.Schema }

func (Event) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "events"}}
}

func (Event) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (Event) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("category_id").Optional().Nillable(),
		field.String("title").NotEmpty().MaxLen(200),
		field.String("summary").Default("").MaxLen(1000),
		field.String("description_markdown").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("tags", []string{}).Default([]string{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("organizer_name").Default("").MaxLen(200),
		field.String("organizer_url").Default("").MaxLen(2048),
		field.String("fee_type").Default(domain.EventFeeUnknown).MaxLen(20),
		field.Float("price_min").Optional().Nillable(),
		field.Float("price_max").Optional().Nillable(),
		field.String("currency").Default("CNY").MaxLen(8),
		field.String("registration_url").Default("").MaxLen(2048),
		field.Time("registration_deadline").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("cover_url").Default("").MaxLen(2048),
		field.String("status").Default(domain.EventStatusDraft).MaxLen(20),
		field.String("visibility").Default(domain.EventVisibilityAuthenticated).MaxLen(20),
		field.JSON("audience", domain.EventAudience{}).Default(domain.EventAudience{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("visible_from").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("visible_until").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("published_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("cancelled_reason").Default("").MaxLen(1000),
		field.JSON("manual_override_fields", []string{}).Default([]string{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int64("created_by").Optional().Nillable(),
		field.Int64("updated_by").Optional().Nillable(),
	}
}

func (Event) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("category", EventCategory.Type).Ref("events").Field("category_id").Unique(),
		edge.To("occurrences", EventOccurrence.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("source_records", EventSourceRecord.Type).Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("import_items", EventImportItem.Type).Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (Event) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "published_at"),
		index.Fields("category_id"),
		index.Fields("visible_from"),
		index.Fields("visible_until"),
		index.Fields("deleted_at"),
	}
}
