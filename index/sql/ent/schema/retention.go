package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Settings is the one row of store settings: the write policy of docs/12
// and the retention defaults of docs/09.
type Settings struct {
	ent.Schema
}

func (Settings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("policy").Optional().Nillable(),
		field.Uint32("policy_version").Default(0),
		field.Time("key_acknowledged_at").Optional().Nillable(),
		field.String("retention_policy").Optional().Nillable(),
		field.Int("hold_days").Default(14),
		field.Int("trash_days").Default(14),
	}
}

// Pin caches a PIN object: the commit it keeps and when it was dropped.
type Pin struct {
	ent.Schema
}

func (Pin) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("ref").Immutable(),
		field.Bytes("target"),
		field.Time("received_at"),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (Pin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ref").Unique(),
		index.Fields("target").Annotations(entsql.IndexWhere("deleted_at IS NULL")).StorageKey("pins_target"),
	}
}

func (Pin) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Checks: map[string]string{
			"pins_ref_width":    "length(ref) = 32",
			"pins_target_width": "length(target) = 32",
		}},
	}
}

// DeletedRef is a ref a tombstone names: written before the tombstone is
// durable, consulted by every commit and pin Put (docs/09).
type DeletedRef struct {
	ent.Schema
}

func (DeletedRef) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("ref").Immutable(),
		field.Time("tombstoned_at"),
	}
}

func (DeletedRef) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ref").Unique(),
	}
}

func (DeletedRef) Annotations() []schema.Annotation {
	return []schema.Annotation{refWidth("deleted_refs", "ref")}
}
