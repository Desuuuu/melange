package migrator

import (
	"fmt"

	"github.com/pthm/melange/lib/sqlgen/sqldsl"
)

// migrationsDDL returns a query that defines the melange_migrations table for
// tracking migration state.
func migrationsDDL(databaseSchema string) string {
	table := sqldsl.PrefixIdent("melange_migrations", databaseSchema)

	return fmt.Sprintf(`-- Melange migrations tracking table
-- Stores migration history for change detection and orphan cleanup.
--
-- Each row represents a completed migration:
-- - melange_version: Version of the melange CLI/library (e.g., "v0.4.3")
-- - schema_checksum: SHA256 of the schema.fga content
-- - codegen_version: Version of the SQL generation logic
-- - function_names: All generated function names (for orphan detection)
--
-- The migrator checks the most recent record to determine if re-migration
-- is needed. If both checksum and codegen_version match, migration is skipped
-- unless --force is specified.

CREATE TABLE IF NOT EXISTS %[1]s (
    id SERIAL PRIMARY KEY,
    migrated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    melange_version VARCHAR(64) NOT NULL DEFAULT '',
    schema_checksum VARCHAR(64) NOT NULL,
    codegen_version VARCHAR(32) NOT NULL,
    function_names TEXT[] NOT NULL
);

-- Lookup by checksum for change detection
CREATE INDEX IF NOT EXISTS idx_melange_migrations_checksum
ON %[1]s (schema_checksum, codegen_version);
`, table)
}

// addMelangeVersionColumn return a migration to add the melange_version column
// to existing tables.
func addMelangeVersionColumn(databaseSchema string) string {
	table := sqldsl.PrefixIdent("melange_migrations", databaseSchema)

	return fmt.Sprintf(`
ALTER TABLE %s
ADD COLUMN IF NOT EXISTS melange_version VARCHAR(64) NOT NULL DEFAULT '';
`, table)
}
