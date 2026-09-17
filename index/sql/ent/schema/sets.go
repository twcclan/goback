// Package schema is the ent schema of the index: the store configuration
// of docs/12, the set caches and retention state of docs/09, the presence
// filters of docs/08 and the archive index of docs/07, in one database.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Set is a backup set: owned by the agent of its first commit, with its
// retention policy and lifecycle state.
type Set struct {
	ent.Schema
}

func (Set) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("name"),
		field.String("agent_id").Optional().Nillable(),
		field.Enum("state").Values("active", "closing", "deleted").Default("active"),
		field.String("retention_policy").Optional().Nillable(),
		field.Bool("retention_paused").Default(false),
		field.Bool("erase").Default(false),
	}
}

func (Set) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("files", File.Type),
		edge.To("trees", Tree.Type),
		edge.To("refs", SetRef.Type),
	}
}

func (Set) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}

// CommitRow is an indexed commit with its receipt order, presence filter,
// verified size and the retention lifecycle of docs/09. (Commit is what
// ent calls ending a transaction.)
type CommitRow struct {
	ent.Schema
}

func (CommitRow) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("ref").Immutable(),
		field.Time("timestamp"),
		field.Time("received_at"),
		field.Bytes("tree"),
		field.Bytes("parent").Optional(),
		field.String("agent_id").Default(""),
		field.Int64("scan_start_ns").Default(0),
		field.Uint32("policy_version").Default(0),
		field.Bool("consistent").Default(false),
		field.Int64("set_id"),
		field.Bytes("presence").Optional(),
		field.Bool("partial").Default(false),
		field.String("retained_by").Default(""),
		field.Time("retire_at").Optional().Nillable(),
		field.Time("deleted_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("tombstoned_at").Optional().Nillable(),
		field.Int64("logical_size").Optional().Nillable(),
		field.JSON("metadata", map[string]string{}).Optional(),
	}
}

func (CommitRow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("set", Set.Type).Unique().Required().Field("set_id"),
	}
}

func (CommitRow) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ref").Unique(),
		index.Fields("set_id", "received_at"),
		index.Fields("set_id").Annotations(entsql.IndexWhere("presence IS NOT NULL")).StorageKey("commits_presence"),
		index.Fields("expires_at").Annotations(entsql.IndexWhere("tombstoned_at IS NULL AND expires_at IS NOT NULL")).StorageKey("commits_expiring"),
	}
}

func (CommitRow) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "commits", Checks: map[string]string{
			"commits_ref_width":  "length(ref) = 32",
			"commits_tree_width": "length(tree) = 32",
		}},
	}
}

// File is one version of a path in a set, valid over the range of commits
// that contain it.
type File struct {
	ent.Schema
}

func (File) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("set_id"),
		field.String("path"),
		field.String("dir"),
		field.Time("valid_from"),
		field.Time("valid_until").Optional().Nillable(),
		field.Bytes("ref"),
		field.Int64("mtime_ns"),
		field.Uint32("mode"),
		field.String("user"),
		field.String("group"),
		field.Int64("size"),
	}
}

func (File) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("set", Set.Type).Ref("files").Unique().Required().Field("set_id"),
	}
}

func (File) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("set_id", "path", "valid_from").Unique(),
		index.Fields("set_id", "dir").Annotations(entsql.IndexWhere("valid_until IS NULL")).StorageKey("files_open"),
	}
}

func (File) Annotations() []schema.Annotation {
	return []schema.Annotation{refWidth("files", "ref")}
}

// Tree is one version of a directory in a set, valid like a File.
type Tree struct {
	ent.Schema
}

func (Tree) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("set_id"),
		field.String("path"),
		field.String("dir"),
		field.Time("valid_from"),
		field.Time("valid_until").Optional().Nillable(),
		field.Bytes("ref"),
	}
}

func (Tree) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("set", Set.Type).Ref("trees").Unique().Required().Field("set_id"),
	}
}

func (Tree) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("set_id", "path", "valid_from").Unique(),
		index.Fields("set_id", "dir").Annotations(entsql.IndexWhere("valid_until IS NULL")).StorageKey("trees_open"),
	}
}

func (Tree) Annotations() []schema.Annotation {
	return []schema.Annotation{refWidth("trees", "ref")}
}

// SetRef records that a set references a commit, tree or file object,
// which is what makes the object readable (docs/08).
type SetRef struct {
	ent.Schema
}

func (SetRef) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("set_id"),
		field.Bytes("ref"),
	}
}

func (SetRef) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("set", Set.Type).Ref("refs").Unique().Required().Field("set_id"),
	}
}

func (SetRef) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("set_id", "ref").Unique(),
		index.Fields("ref"),
	}
}

func (SetRef) Annotations() []schema.Annotation {
	return []schema.Annotation{refWidth("set_refs", "ref")}
}

// refWidth is the check every ref column carries.
func refWidth(table, column string) schema.Annotation {
	return entsql.Annotation{Checks: map[string]string{
		table + "_" + column + "_width": "length(" + column + ") = 32",
	}}
}

func cascade() entsql.Annotation {
	return entsql.Annotation{OnDelete: entsql.Cascade}
}
