package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pthm/melange/pkg/migrator"
	"github.com/pthm/melange/test/testutil"
)

// TestModularSchema_FullPipeline tests the complete fga.mod → parse → migrate → check path.
// This exercises modular model support end-to-end with a real PostgreSQL database.
func TestModularSchema_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Set up modular schema files in a temp directory
	dir := t.TempDir()
	writeFile(t, dir, "fga.mod", `schema: '1.2'
contents:
  - core.fga
  - tracker/projects.fga
  - wiki/spaces.fga
`)
	writeFile(t, dir, "core.fga", `module core

type user

type organization
  relations
    define member: [user]
    define admin: [user]
`)
	writeFile(t, dir, "tracker/projects.fga", `module tracker

extend type organization
  relations
    define can_create_project: admin

type project
  relations
    define organization: [organization]
    define owner: [user]
    define viewer: member from organization
`)
	writeFile(t, dir, "wiki/spaces.fga", `module wiki

extend type organization
  relations
    define can_create_space: admin or member

type space
  relations
    define organization: [organization]
    define editor: [user]
    define viewer: member from organization
`)

	// Get an empty database and set up tuples infrastructure
	db := testutil.EmptyDB(t)
	ctx := context.Background()

	// Create a simple melange_tuples table (direct tuple storage)
	_, err := db.ExecContext(ctx, `
		CREATE TABLE melange_tuples (
			subject_type TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			subject_relation TEXT NOT NULL DEFAULT '',
			relation TEXT NOT NULL,
			object_type TEXT NOT NULL,
			object_id TEXT NOT NULL
		)
	`)
	require.NoError(t, err, "creating melange_tuples table")

	// Migrate using fga.mod
	err = migrator.Migrate(ctx, db, filepath.Join(dir, "fga.mod"))
	require.NoError(t, err, "migration from fga.mod should succeed")

	// Insert tuples
	tuples := []struct {
		subjectType, subjectID, subjectRelation, relation, objectType, objectID string
	}{
		// Organization membership
		{"user", "alice", "", "admin", "organization", "acme"},
		{"user", "bob", "", "member", "organization", "acme"},
		// Project setup
		{"organization", "acme", "", "organization", "project", "atlas"},
		{"user", "alice", "", "owner", "project", "atlas"},
		// Space setup
		{"organization", "acme", "", "organization", "space", "wiki-main"},
		{"user", "alice", "", "editor", "space", "wiki-main"},
	}
	for _, tuple := range tuples {
		_, err := db.ExecContext(ctx,
			`INSERT INTO melange_tuples (subject_type, subject_id, subject_relation, relation, object_type, object_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			tuple.subjectType, tuple.subjectID, tuple.subjectRelation, tuple.relation, tuple.objectType, tuple.objectID,
		)
		require.NoError(t, err, "inserting tuple")
	}

	// Test permission checks using the generated check_permission function
	checks := []struct {
		name        string
		subjectType string
		subjectID   string
		relation    string
		objectType  string
		objectID    string
		expected    bool
	}{
		// Extended relation from tracker module: can_create_project = admin
		{"admin can create project", "user", "alice", "can_create_project", "organization", "acme", true},
		{"member cannot create project", "user", "bob", "can_create_project", "organization", "acme", false},
		// Extended relation from wiki module: can_create_space = admin or member
		{"admin can create space", "user", "alice", "can_create_space", "organization", "acme", true},
		{"member can create space", "user", "bob", "can_create_space", "organization", "acme", true},
		{"outsider cannot create space", "user", "eve", "can_create_space", "organization", "acme", false},
		// TTU from tracker module: project.viewer = member from organization
		{"member can view project", "user", "bob", "viewer", "project", "atlas", true},
		{"outsider cannot view project", "user", "eve", "viewer", "project", "atlas", false},
		// TTU from wiki module: space.viewer = member from organization
		{"member can view space", "user", "bob", "viewer", "space", "wiki-main", true},
		{"outsider cannot view space", "user", "eve", "viewer", "space", "wiki-main", false},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			var result int
			err := db.QueryRowContext(ctx,
				"SELECT check_permission($1, $2, $3, $4, $5)",
				check.subjectType, check.subjectID, check.relation, check.objectType, check.objectID,
			).Scan(&result)
			require.NoError(t, err)
			allowed := result == 1
			assert.Equal(t, check.expected, allowed, "%s: check_permission(%s:%s, %s, %s:%s)",
				check.name, check.subjectType, check.subjectID, check.relation, check.objectType, check.objectID)
		})
	}
}

// TestModularSchema_SkipDetection verifies that migration skip detection works
// correctly with fga.mod manifests (hashing all module files).
func TestModularSchema_SkipDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	writeFile(t, dir, "fga.mod", `schema: '1.2'
contents:
  - core.fga
`)
	writeFile(t, dir, "core.fga", `module core

type user

type document
  relations
    define viewer: [user]
`)

	db := testutil.EmptyDB(t)
	ctx := context.Background()

	// Create tuples table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE melange_tuples (
			subject_type TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			subject_relation TEXT NOT NULL DEFAULT '',
			relation TEXT NOT NULL,
			object_type TEXT NOT NULL,
			object_id TEXT NOT NULL
		)
	`)
	require.NoError(t, err)

	modPath := filepath.Join(dir, "fga.mod")

	// First migration should apply
	skipped, err := migrator.MigrateWithOptions(ctx, db, modPath, migrator.MigrateOptions{})
	require.NoError(t, err)
	assert.False(t, skipped, "first migration should not be skipped")

	// Second migration with same content should skip
	skipped, err = migrator.MigrateWithOptions(ctx, db, modPath, migrator.MigrateOptions{})
	require.NoError(t, err)
	assert.True(t, skipped, "second migration should be skipped (unchanged)")

	// Modify a module file — migration should apply again
	writeFile(t, dir, "core.fga", `module core

type user

type document
  relations
    define viewer: [user]
    define editor: [user]
`)

	skipped, err = migrator.MigrateWithOptions(ctx, db, modPath, migrator.MigrateOptions{})
	require.NoError(t, err)
	assert.False(t, skipped, "migration should apply after module change")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", name, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
