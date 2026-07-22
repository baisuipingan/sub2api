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

type EventOccurrence struct{ ent.Schema }

func (EventOccurrence) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_occurrences"}}
}

func (EventOccurrence) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (EventOccurrence) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("event_id"),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ends_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("timezone").Default("Asia/Shanghai").MaxLen(64),
		field.Bool("all_day").Default(false),
		field.String("location_mode").Default(domain.EventLocationOffline).MaxLen(20),
		field.String("online_url").Default("").MaxLen(2048),
		field.String("venue_name").Default("").MaxLen(300),
		field.String("address").Default("").MaxLen(1000),
		field.String("country").Default("中国").MaxLen(100),
		field.String("province").Default("").MaxLen(100),
		field.String("city").Default("").MaxLen(100),
		field.String("district").Default("").MaxLen(100),
		field.Float("latitude").Optional().Nillable(),
		field.Float("longitude").Optional().Nillable(),
		field.String("coordinate_source").Default(domain.EventCoordinateWGS84).MaxLen(20),
		field.String("geocode_status").Default("").MaxLen(32),
		field.String("geocode_precision").Default("").MaxLen(32),
		field.String("provider_place_id").Default("").MaxLen(255),
	}
}

func (EventOccurrence) Edges() []ent.Edge {
	return []ent.Edge{edge.From("event", Event.Type).Ref("occurrences").Field("event_id").Unique().Required()}
}

func (EventOccurrence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("event_id", "starts_at"),
		index.Fields("starts_at", "ends_at"),
		index.Fields("city", "starts_at"),
		index.Fields("longitude", "latitude"),
	}
}
