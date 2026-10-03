package store

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/3to1go/scout/internal/testutil"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrateLegacyKeysWritesCurrentKeys(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewSettingsStore(db)
	mock.ExpectQuery("SELECT payload FROM app_settings").WillReturnRows(sqlmock.NewRows([]string{"payload"}).
		AddRow(`{"edge_id":"laptop","central_url":"http://station:6555","edge_credential":"tok","scout_credential":"new"}`))
	mock.ExpectExec("UPDATE app_settings SET payload").WithArgs(payloadMatcher(func(p map[string]string) bool {
		_, oldID := p["edge_id"]
		_, oldURL := p["central_url"]
		_, oldCred := p["edge_credential"]
		return p["scout_id"] == "laptop" && p["station_url"] == "http://station:6555" &&
			p["scout_credential"] == "new" && !oldID && !oldURL && !oldCred
	})).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.MigrateLegacyKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyKeysSkipsCurrentSettings(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewSettingsStore(db)
	mock.ExpectQuery("SELECT payload FROM app_settings").WillReturnRows(sqlmock.NewRows([]string{"payload"}).
		AddRow(`{"scout_id":"laptop"}`))
	if err := s.MigrateLegacyKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyKeysWithoutSettings(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewSettingsStore(db)
	mock.ExpectQuery("SELECT payload FROM app_settings").WillReturnRows(sqlmock.NewRows([]string{"payload"}))
	if err := s.MigrateLegacyKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// payloadMatcher matches a JSON settings payload argument by its decoded string fields.
type payloadMatcher func(map[string]string) bool

func (m payloadMatcher) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	var p map[string]string
	return json.Unmarshal([]byte(s), &p) == nil && m(p)
}
