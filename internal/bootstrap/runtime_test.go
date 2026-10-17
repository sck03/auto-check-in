package bootstrap

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/port"
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
	config.DataDir = "custom-data"
	want, _ := filepath.Abs("custom-data")
	if dir, err = config.DataDirectory(); err != nil || dir != want {
		t.Fatalf("explicit path lost: %s, %v", dir, err)
	}
}

func TestRuntimeReportsDirectoryAndInitializesDesktopAfterStorage(t *testing.T) {
	dir := t.TempDir()
	initialized := false
	rt, err := Start(Config{DataDir: dir, Listen: "127.0.0.1:0", NewDesktop: func(readyDir string) (port.DesktopIntegration, error) {
		if readyDir != dir {
			t.Fatalf("desktop received wrong directory: %s", readyDir)
		}
		for _, name := range []string{"master.key", "checkin.db"} {
			if _, err := os.Stat(filepath.Join(readyDir, name)); err != nil {
				t.Fatal(err)
			}
		}
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
