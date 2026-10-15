package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxReleaseMembersAndPermissions(t *testing.T) {
	source := t.TempDir()
	for name, content := range map[string]string{
		"checkin-server": "executable contents",
		"README.md":      "usage",
		"stale-file":     "must not ship",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), "release.tar.gz")
	if err := createArchive(source, output, "checkin-server", []string{"checkin-server", "README.md"}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	for _, want := range []struct {
		name    string
		mode    int64
		content string
	}{
		{"checkin-server", 0o755, "executable contents"},
		{"README.md", 0o644, "usage"},
	} {
		header, err := archive.Next()
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != want.name || header.Mode != want.mode || header.Typeflag != tar.TypeReg {
			t.Fatalf("unexpected member: %+v", header)
		}
		content, err := io.ReadAll(archive)
		if err != nil || string(content) != want.content {
			t.Fatalf("content %q, error %v", content, err)
		}
	}
	if header, err := archive.Next(); err != io.EOF {
		t.Fatalf("unexpected extra member: %+v, error %v", header, err)
	}
}
