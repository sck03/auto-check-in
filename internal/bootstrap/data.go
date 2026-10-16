package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

// DataDirectory resolves the portable default independently of the working
// directory, including when Windows starts the program at login.
func (c Config) DataDirectory() (string, error) {
	dir := c.DataDir
	if dir == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("无法确定程序目录，请使用 -data 指定数据目录：%w", err)
		}
		dir = filepath.Join(filepath.Dir(executable), "data")
	}
	return filepath.Abs(dir)
}

// The caller holds the destination's instance lock. Keep the source intact for
// rollback; publish the database last so an interrupted copy can be retried.
func migrateLegacyData(source, destination, configuredKey string) error {
	if _, err := os.Stat(filepath.Join(destination, "checkin.db")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Join(source, "checkin.db")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	for _, name := range []string{"checkin.db-wal", "checkin.db-shm"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err == nil {
			return fmt.Errorf("新目录已有 %s 但缺少数据库，请先恢复该目录的数据", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	release, err := security.LockDirectory(source)
	if err != nil {
		return fmt.Errorf("请先退出使用旧目录 %s 的程序：%w", source, err)
	}
	defer release()
	key, err := security.LoadKey(source, configuredKey)
	if err != nil {
		return err
	}
	vault, err := security.NewVault(key)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(destination, ".migration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	// A stopped instance may still have committed changes in its WAL. SHM and
	// browser profiles are disposable and must not be carried into the new home.
	for _, name := range []string{"checkin.db", "checkin.db-wal", "master.key", "access.token", "desktop.json"} {
		err = copyDataFile(filepath.Join(source, name), filepath.Join(staging, name))
		if os.IsNotExist(err) && name != "checkin.db" {
			continue
		}
		if err != nil {
			return err
		}
	}
	repo, err := store.Open(filepath.Join(staging, "checkin.db"), vault)
	if err != nil {
		return err
	}
	verifyErr := repo.Verify(context.Background())
	closeErr := repo.Close() // The last SQLite connection checkpoints the copied WAL.
	if verifyErr != nil {
		return verifyErr
	}
	if closeErr != nil {
		return closeErr
	}
	var pending []string
	for _, name := range []string{"master.key", "access.token", "desktop.json", "checkin.db"} {
		staged, target := filepath.Join(staging, name), filepath.Join(destination, name)
		if _, err := os.Stat(staged); os.IsNotExist(err) {
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("新目录已有 %s，旧目录没有对应文件，未覆盖", name)
			} else if !os.IsNotExist(err) {
				return err
			}
			continue
		} else if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil {
			// A previous attempt may have published companion files before a
			// crash. Only accept identical files; never replace another key.
			copied, err := os.ReadFile(staged)
			if err != nil {
				return err
			}
			if !bytes.Equal(existing, copied) {
				return fmt.Errorf("新目录已有不同的 %s，未覆盖；请检查数据目录或使用 -data 指定空目录", name)
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		pending = append(pending, name)
	}
	for _, name := range pending {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	slog.Info("legacy data copied and verified", "source", source, "data_dir", destination)
	return nil
}

func copyDataFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("数据文件不是普通文件：%s", source)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return output.Close()
}
