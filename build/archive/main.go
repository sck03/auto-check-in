// Command archive creates a flat Linux release archive with Unix permissions,
// including when the release is built on Windows.
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	source := flag.String("source", "", "directory containing release files")
	output := flag.String("output", "", "destination .tar.gz file")
	executable := flag.String("executable", "", "member to mark executable")
	flag.Parse()
	if *source == "" || *output == "" || *executable == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "source, output, executable and member names are required")
		os.Exit(2)
	}
	if err := createArchive(*source, *output, *executable, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func createArchive(source, output, executable string, members []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed := gzip.NewWriter(file)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	for _, name := range members {
		if err := addFile(archive, source, name, name == executable); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	return file.Close()
}

func addFile(archive *tar.Writer, source, name string, executable bool) error {
	if name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("archive members must be plain filenames: %q", name)
	}
	file, err := os.Open(filepath.Join(source, name))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("archive member must be a regular file: %q", name)
	}
	mode := int64(0o644)
	if executable {
		mode = 0o755
	}
	if err := archive.WriteHeader(&tar.Header{
		Name: name, Mode: mode, Size: info.Size(), Typeflag: tar.TypeReg,
		ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR,
	}); err != nil {
		return err
	}
	_, err = io.Copy(archive, file)
	return err
}
