package domain

import "fmt"

// Creation and backup restore share the same capacity policy. Existing data
// remains readable; export fails explicitly rather than creating an unusable file.
const (
	MaxSites              = 1000
	MaxAccounts           = 10000
	MaxBackupRuns         = 100000
	BackupVersion         = 1
	MaxBackupBytes        = 32 << 20
	MaxBackupArchiveBytes = 48 << 20
	MaxRetryAfterSeconds  = 86400
)

func ValidateBackupCounts(sites, accounts, runs int) error {
	for _, limit := range []struct {
		name       string
		count, max int
	}{
		{"站点", sites, MaxSites}, {"账号", accounts, MaxAccounts}, {"历史记录", runs, MaxBackupRuns},
	} {
		if limit.count > limit.max {
			return fmt.Errorf("%s数量超过上限 %d，请整理后再导出或恢复", limit.name, limit.max)
		}
	}
	return nil
}
