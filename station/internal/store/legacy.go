package store

import "context"

// legacyRenameSQL renames the tables, columns, indexes, constraints, and saved JSON keys of a
// database created by an older release, before the apps took their current names. Each rename
// runs only while the old name exists and the new one doesn't, so the block is safe to run on
// every start.
const legacyRenameSQL = `
DO $$
DECLARE
	r record;
BEGIN
	IF to_regclass('edge_credentials') IS NOT NULL AND to_regclass('scout_credentials') IS NULL THEN
		ALTER TABLE edge_credentials RENAME TO scout_credentials;
	END IF;
	IF to_regclass('edge_registration') IS NOT NULL AND to_regclass('scout_registration') IS NULL THEN
		ALTER TABLE edge_registration RENAME TO scout_registration;
	END IF;

	FOR r IN
		SELECT c.table_name, c.column_name
		FROM information_schema.columns c
		WHERE c.table_schema = current_schema()
			AND c.table_name IN ('snapshot_index', 'scout_registration', 'archive_size_history')
			AND c.column_name IN ('edge_id', 'edge_instance_id')
			AND NOT EXISTS (
				SELECT 1 FROM information_schema.columns n
				WHERE n.table_schema = c.table_schema AND n.table_name = c.table_name
					AND n.column_name = replace(c.column_name, 'edge_', 'scout_'))
	LOOP
		EXECUTE format('ALTER TABLE %I RENAME COLUMN %I TO %I',
			r.table_name, r.column_name, replace(r.column_name, 'edge_', 'scout_'));
	END LOOP;

	-- Postgres named these when the table was created, and keeps the names through a table rename.
	FOR r IN
		SELECT conname FROM pg_constraint
		WHERE conrelid = to_regclass('scout_credentials') AND conname LIKE 'edge\_credentials\_%'
	LOOP
		EXECUTE format('ALTER TABLE scout_credentials RENAME CONSTRAINT %I TO %I',
			r.conname, replace(r.conname, 'edge_', 'scout_'));
	END LOOP;

	FOR r IN
		SELECT old_name, new_name FROM (VALUES
			('idx_edge_credentials_expires_at', 'idx_scout_credentials_expires_at'),
			('idx_edge_registration_instance', 'idx_scout_registration_instance'),
			('idx_edge_registration_credential_hash', 'idx_scout_registration_credential_hash')
		) AS v(old_name, new_name)
	LOOP
		IF to_regclass(r.old_name) IS NOT NULL AND to_regclass(r.new_name) IS NULL THEN
			EXECUTE format('ALTER INDEX %I RENAME TO %I', r.old_name, r.new_name);
		END IF;
	END LOOP;

	-- JSON payloads keep their keys through a schema rename, so rename those keys too. A key
	-- that already has its current name keeps its value.
	IF to_regclass('app_settings') IS NOT NULL THEN
		UPDATE app_settings
		SET payload = (payload - 'ntfy_match_edge_id' - 'ntfy_match_edge_instance_id')
			|| jsonb_strip_nulls(jsonb_build_object(
				'ntfy_match_scout_id', COALESCE(payload->'ntfy_match_scout_id', payload->'ntfy_match_edge_id'),
				'ntfy_match_scout_instance_id', COALESCE(payload->'ntfy_match_scout_instance_id', payload->'ntfy_match_edge_instance_id')))
		WHERE payload ?| ARRAY['ntfy_match_edge_id', 'ntfy_match_edge_instance_id'];
	END IF;
	IF to_regclass('upload_sessions') IS NOT NULL THEN
		UPDATE upload_sessions
		SET payload = (payload - 'edge_id' - 'edge_instance_id')
			|| jsonb_strip_nulls(jsonb_build_object(
				'scout_id', COALESCE(payload->'scout_id', payload->'edge_id'),
				'scout_instance_id', COALESCE(payload->'scout_instance_id', payload->'edge_instance_id')))
		WHERE payload ?| ARRAY['edge_id', 'edge_instance_id'];
	END IF;
END $$`

// MigrateLegacyNames renames database objects and saved JSON keys from an older release. Run it
// before the stores' EnsureSchema, which would otherwise create empty tables under the new names.
func MigrateLegacyNames(ctx context.Context, pool dbPool) error {
	_, err := pool.Exec(ctx, legacyRenameSQL)
	return err
}
