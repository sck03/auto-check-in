package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

func TestDataDirectoryUsesExecutableAndHonorsOverrides(t *testing.T) {
	t.Setenv("CHECKIN_DATA_DIR", "")
	t.Chdir(t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := EnvConfig()
	dir, err := config.DataDirectory()
	if err != nil || dir != filepath.Join(filepath.Dir(executable), "data") {
		t.Fatalf("default followed the working directory: %s, %v", dir, err)
	}
	custom := t.TempDir()
	t.Setenv("CHECKIN_DATA_DIR", custom)
	config = EnvConfig()
	if dir, err = config.DataDirectory(); err != nil || dir != custom {
		t.Fatalf("environment override lost: %s, %v", dir, err)
	}
	config.DataDir = "custom-data" // The command line overrides the environment.
	want, _ := filepath.Abs("custom-data")
	if dir, err = config.DataDirectory(); err != nil || dir != want {
		t.Fatalf("explicit path lost: %s, %v", dir, err)
	}
}

func legacyFixture(t *testing.T) (string, domain.Credentials) {
	t.Helper()
	dir := t.TempDir()
	key, err := security.LoadKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(key)
	repo, err := store.Open(filepath.Join(dir, "checkin.db"), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	creds := domain.Credentials{Method: "cookie", APIUser: "fixture", CookieHeader: "session=migration-fixture"}
	account := domain.Account{ID: "fixture", SiteID: "anyrouter", Name: "迁移账号", Enabled: true, Schedule: domain.DefaultSchedule()}
	if _, err = repo.SaveAccount(context.Background(), account, creds, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Enqueue(context.Background(), account.ID, "manual", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = security.AccessToken(dir, ""); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "desktop.json"), []byte(`{"minimize_to_tray":false,"close_to_tray":true,"version":4}`), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, creds
}

func readData(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openMigrated(t *testing.T, dir, keyText string) *store.Store {
	t.Helper()
	key, err := security.LoadKey(dir, keyText)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(key)
	repo, err := store.Open(filepath.Join(dir, "checkin.db"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestLegacyMigrationPreservesDataAndResumesPartialCopy(t *testing.T) {
	for _, resume := range []bool{false, true} {
		name := "first-copy"
		if resume {
			name = "resume"
		}
		t.Run(name, func(t *testing.T) {
			source, creds := legacyFixture(t)
			destination := t.TempDir()
			original := map[string][]byte{}
			for _, name := range []string{"checkin.db", "master.key", "access.token", "desktop.json"} {
				original[name] = readData(t, filepath.Join(source, name))
			}
			if resume {
				if err := copyDataFile(filepath.Join(source, "master.key"), filepath.Join(destination, "master.key")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(source, "endpoint.json"), []byte(`{"url":"http://127.0.0.1:1"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := migrateLegacyData(source, destination, ""); err != nil {
				t.Fatal(err)
			}
			for name, data := range original {
				if !bytes.Equal(readData(t, filepath.Join(source, name)), data) {
					t.Fatalf("migration changed the original %s", name)
				}
				if name != "checkin.db" && !bytes.Equal(readData(t, filepath.Join(destination, name)), data) {
					t.Fatalf("migration lost %s", name)
				}
			}
			repo := openMigrated(t, destination, "")
			got, err := repo.Credentials(context.Background(), "fixture")
			if err != nil || !reflect.DeepEqual(got, creds) {
				t.Fatal("migrated credentials did not decrypt", err)
			}
			history, err := repo.Runs(context.Background(), domain.RunFilter{})
			if err != nil || history.Total != 1 || history.Items[0].Status != domain.Queued {
				t.Fatalf("migration changed execution history: %+v %v", history, err)
			}
			if err := repo.Close(); err != nil {
				t.Fatal(err)
			}
			before := readData(t, filepath.Join(destination, "checkin.db"))
			if err := migrateLegacyData(source, destination, "invalid-but-unused"); err != nil {
				t.Fatal("existing portable data tried to migrate again", err)
			}
			if !bytes.Equal(before, readData(t, filepath.Join(destination, "checkin.db"))) {
				t.Fatal("existing portable database was replaced")
			}
			if _, err := os.Stat(filepath.Join(destination, "endpoint.json")); !os.IsNotExist(err) {
				t.Fatal("stale endpoint was copied", err)
			}
			if leftovers, _ := filepath.Glob(filepath.Join(destination, ".migration-*")); len(leftovers) != 0 {
				t.Fatal("migration staging was not cleaned")
			}
		})
	}
}

func TestLegacyMigrationIncludesCommittedWAL(t *testing.T) {
	source, _ := legacyFixture(t)
	writer, err := sql.Open("sqlite", filepath.Join(source, "checkin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	// Keep an idle connection open to retain committed WAL pages, as left by an
	// unclean exit. No writes run concurrently with the migration.
	if _, err := writer.Exec("PRAGMA wal_autocheckpoint=0; UPDATE accounts SET name='WAL-only change' WHERE id='fixture'"); err != nil {
		t.Fatal(err)
	}
	if wal := readData(t, filepath.Join(source, "checkin.db-wal")); len(wal) == 0 {
		t.Fatal("fixture did not leave WAL data")
	}
	destination := t.TempDir()
	if err := migrateLegacyData(source, destination, ""); err != nil {
		t.Fatal(err)
	}
	repo := openMigrated(t, destination, "")
	account, err := repo.Account(context.Background(), "fixture")
	if err != nil || account.Name != "WAL-only change" {
		t.Fatal("committed WAL changes were lost", err)
	}
}

func TestLegacyMigrationRefusesUnsafeCopies(t *testing.T) {
	for _, scenario := range []string{"locked", "missing-key", "wrong-key", "corrupt-db", "different-key", "orphan-wal"} {
		t.Run(scenario, func(t *testing.T) {
			source, _ := legacyFixture(t)
			destination, configuredKey := t.TempDir(), ""
			var err error
			switch scenario {
			case "locked":
				var release func()
				release, err = security.LockDirectory(source)
				if err == nil {
					defer release()
				}
			case "missing-key":
				err = os.Remove(filepath.Join(source, "master.key"))
			case "wrong-key":
				configuredKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
			case "corrupt-db":
				err = os.WriteFile(filepath.Join(source, "checkin.db"), []byte("broken database"), 0600)
			case "different-key":
				err = os.WriteFile(filepath.Join(destination, "master.key"), []byte("existing key"), 0600)
			case "orphan-wal":
				err = os.WriteFile(filepath.Join(destination, "checkin.db-wal"), []byte("existing WAL"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := readData(t, filepath.Join(source, "checkin.db"))
			if err := migrateLegacyData(source, destination, configuredKey); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			if !bytes.Equal(before, readData(t, filepath.Join(source, "checkin.db"))) {
				t.Fatal("failed migration changed original data")
			}
			if _, err := os.Stat(filepath.Join(destination, "checkin.db")); !os.IsNotExist(err) {
				t.Fatal("failed migration published a database", err)
			}
			if scenario == "different-key" && string(readData(t, filepath.Join(destination, "master.key"))) != "existing key" {
				t.Fatal("failed migration replaced the destination key")
			}
		})
	}
}

func TestLegacyMigrationWithExternalKey(t *testing.T) {
	source, _ := legacyFixture(t)
	key := string(readData(t, filepath.Join(source, "master.key")))
	if err := os.Remove(filepath.Join(source, "master.key")); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := migrateLegacyData(source, destination, key); err != nil {
		t.Fatal(err)
	}
	if err := openMigrated(t, destination, key).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "master.key")); !os.IsNotExist(err) {
		t.Fatal("external key was written to disk", err)
	}
}

func TestRuntimeReportsDirectoryAndInitializesDesktopAfterStorage(t *testing.T) {
	dir := t.TempDir()
	initialized := false
	rt, err := Start(Config{DataDir: dir, Listen: "127.0.0.1:0", NewDesktop: func(readyDir string) (port.DesktopIntegration, error) {
		if readyDir != dir {
			t.Fatalf("desktop received wrong directory: %s", readyDir)
		}
		readData(t, filepath.Join(readyDir, "master.key"))
		readData(t, filepath.Join(readyDir, "checkin.db"))
		initialized = true
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	request, _ := http.NewRequest(http.MethodGet, rt.URL+"/api/v1/meta", nil)
	request.Header.Set("Authorization", "Bearer "+rt.Token)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var meta struct {
		DataDir string `json:"data_dir"`
	}
	if err := json.NewDecoder(response.Body).Decode(&meta); err != nil || meta.DataDir != dir || !initialized {
		t.Fatalf("runtime metadata or initialization failed: %+v, %v", meta, err)
	}
}
