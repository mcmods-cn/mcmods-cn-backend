package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type revisionPublicIDQueryStub struct {
	publicID string
	err      error
	called   bool
}

func (query *revisionPublicIDQueryStub) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	query.called = true
	return revisionPublicIDRowStub{publicID: query.publicID, err: query.err}
}

type revisionPublicIDRowStub struct {
	publicID string
	err      error
}

func (row revisionPublicIDRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*string) = row.publicID
	return nil
}

func TestRevisionPublicIDValuePreservesLookupErrors(t *testing.T) {
	internalID := int64(42)
	wantErr := errors.New("revision route unavailable")
	failingQuery := &revisionPublicIDQueryStub{err: wantErr}
	publicID, err := revisionPublicIDValue(context.Background(), failingQuery, &internalID)
	if publicID != nil || !errors.Is(err, wantErr) {
		t.Fatalf("lookup failure = (%v, %v), want (nil, %v)", publicID, err, wantErr)
	}

	successfulQuery := &revisionPublicIDQueryStub{publicID: "revision-public-id"}
	publicID, err = revisionPublicIDValue(context.Background(), successfulQuery, &internalID)
	if err != nil || publicID == nil || *publicID != "revision-public-id" {
		t.Fatalf("successful lookup = (%v, %v), want revision-public-id", publicID, err)
	}

	nilQuery := &revisionPublicIDQueryStub{err: errors.New("must not query")}
	publicID, err = revisionPublicIDValue(context.Background(), nilQuery, nil)
	if err != nil || publicID != nil || nilQuery.called {
		t.Fatalf("nil revision = (%v, %v, called=%t), want (nil, nil, false)", publicID, err, nilQuery.called)
	}
}
