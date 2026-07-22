package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EventCategory struct{ ent.Schema }

func (EventCategory) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_categories"}}
}

func (EventCategory) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (EventCategory) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").NotEmpty().MaxLen(64),
		field.String("name").NotEmpty().MaxLen(100),
		field.String("color").Default("#2563EB").MaxLen(20),
		field.String("icon").Default("calendar").MaxLen(64),
		field.Int("sort_order").Default(0),
		field.Bool("enabled").Default(true),
	}
}

func (EventCategory) Edges() []ent.Edge {
	return []ent.Edge{edge.To("events", Event.Type)}
}

func (EventCategory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code"),
		index.Fields("enabled", "sort_order"),
		index.Fields("deleted_at"),
	}
}
