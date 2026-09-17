package ent

//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock,sql/execquery,sql/versioned-migration ./schema
