package schema

import (
	"time"

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

// UserUsageCard holds the schema definition for a user's usage card balance.
type UserUsageCard struct {
	ent.Schema
}

func (UserUsageCard) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_usage_cards"},
	}
}

func (UserUsageCard) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.SoftDeleteMixin{},
	}
}

func (UserUsageCard) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("plan_id").
			Optional().
			Nillable(),
		field.String("name").
			MaxLen(100).
			Default(""),
		field.Time("starts_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("total_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("used_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		field.String("status").
			MaxLen(20).
			Default(domain.UsageCardStatusActive),
		field.String("source").
			MaxLen(20).
			Default(domain.UsageCardSourceAdmin),
		field.Int64("source_order_id").
			Optional().
			Nillable(),
		field.String("source_redeem_code").
			MaxLen(64).
			Optional().
			Nillable(),
		field.Int64("assigned_by").
			Optional().
			Nillable(),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserUsageCard) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("usage_cards").
			Field("user_id").
			Unique().
			Required(),
		edge.From("plan", UsageCardPlan.Type).
			Ref("usage_cards").
			Field("plan_id").
			Unique(),
		edge.From("assigned_by_user", User.Type).
			Ref("assigned_usage_cards").
			Field("assigned_by").
			Unique(),
		edge.To("usage_logs", UsageLog.Type),
	}
}

func (UserUsageCard) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("plan_id"),
		index.Fields("status"),
		index.Fields("expires_at"),
		index.Fields("user_id", "status", "expires_at"),
		index.Fields("source_order_id"),
		index.Fields("source_redeem_code"),
		index.Fields("assigned_by"),
		index.Fields("deleted_at"),
	}
}
