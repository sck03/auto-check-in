package store

import (
	"context"

	"auto-check-in/internal/domain"
)

// Equivalent URL spellings in existing v5 data are normalized once at startup.
// Stored localStorage keys are normalized lazily when credentials are read.
func (s *Store) normalizeOrigins() error {
	sites, err := s.Sites(context.Background())
	if err != nil {
		return err
	}
	for _, site := range sites {
		origin := domain.CanonicalOrigin(site.BaseURL)
		if origin == site.BaseURL {
			continue
		}
		site.BaseURL = origin
		if _, err := s.db.Exec("UPDATE sites SET config=? WHERE id=?", jsonText(site), site.ID); err != nil {
			return err
		}
	}
	return nil
}
