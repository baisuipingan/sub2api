//go:build unit

package repository

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDesktopAgentManagementMetadataFailsClosed(t *testing.T) {
	for _, kind := range []string{"query error", "missing row", "invalid type", "scan error", "success"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			query := mock.ExpectQuery("SELECT id, key_type, COALESCE")
			switch kind {
			case "query error":
				query.WillReturnError(errors.New("metadata unavailable"))
			case "missing row":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "key_type", "purpose"}))
			case "invalid type":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "key_type", "purpose"}).AddRow(9, "unknown", ""))
			case "scan error":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "key_type", "purpose"}).AddRow("invalid", "system", "desktop_agent"))
			case "success":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "key_type", "purpose"}).AddRow(9, "system", "desktop_agent"))
			}
			key := &service.APIKey{ID: 9, Key: "fixture-private"}
			err = attachAPIKeyManagementMetadata(context.Background(), db, []*service.APIKey{key})
			if kind == "success" {
				require.NoError(t, err)
				require.Equal(t, service.APIKeyPurposeDesktopAgent, key.Purpose)
				require.Equal(t, service.APIKeyTypeSystem, key.KeyType)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
