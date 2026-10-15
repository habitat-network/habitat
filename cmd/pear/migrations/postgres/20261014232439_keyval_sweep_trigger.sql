-- +goose Up
-- Atlas cannot generate triggers from GORM models, so this one is hand-written.
-- It reclaims expired key_values rows on write; readers filter on expiry
-- regardless, so it only bounds disk use. SQLite has no triggers; keyval.New
-- sweeps there at startup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION keyval_sweep_expired() RETURNS trigger AS $$
BEGIN
    IF pg_try_advisory_xact_lock(hashtext('keyval.sweep')) THEN
        DELETE FROM key_values WHERE expires_at <= now();
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS keyval_sweep_expired_trg ON key_values;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER keyval_sweep_expired_trg
AFTER INSERT OR UPDATE ON key_values
FOR EACH STATEMENT EXECUTE FUNCTION keyval_sweep_expired();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS keyval_sweep_expired_trg ON key_values;
-- +goose StatementEnd

-- +goose StatementBegin
DROP FUNCTION IF EXISTS keyval_sweep_expired();
-- +goose StatementEnd
