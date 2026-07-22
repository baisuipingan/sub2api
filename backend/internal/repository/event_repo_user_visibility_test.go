package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbevent "github.com/Wei-Shaw/sub2api/ent/event"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type eventQueryCaptureMatcher struct {
	actual *[]string
}

func (m eventQueryCaptureMatcher) Match(_, actual string) error {
	if m.actual == nil {
		return fmt.Errorf("query capture target is nil")
	}
	*m.actual = append(*m.actual, actual)
	return nil
}

func TestListPublishedForUserFiltersAudienceAndMatchingOccurrences(t *testing.T) {
	queries := make([]string, 0, 2)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(eventQueryCaptureMatcher{actual: &queries}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &eventRepository{client: client}

	mock.ExpectQuery("count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("events").WillReturnRows(sqlmock.NewRows(dbevent.Columns))

	from := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	_, _, err = repo.ListPublishedForUser(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, service.EventListFilters{
		City: "上海",
		From: &from,
	}, []int64{7})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Len(t, queries, 2)

	combined := normalizeSQLWhitespace(strings.Join(queries, " "))
	require.Contains(t, combined, `"visibility" = $`)
	require.Contains(t, combined, `"audience" @> $`)
	require.Contains(t, combined, `::jsonb`)
	require.Contains(t, combined, `event_occurrences`)
	require.Contains(t, combined, `"city" ILIKE`)
	require.Contains(t, combined, `"ends_at" IS NULL`)
}
