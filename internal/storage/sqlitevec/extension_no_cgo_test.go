//go:build !cgo

package sqlitevec

import (
	"errors"
	"testing"
)

func TestOpenReturnsUnavailableErrorWithoutCGO(t *testing.T) {
	backend, err := Open(":memory:")
	if backend != nil {
		t.Fatal("expected nil backend when sqlite-vec cgo bindings are unavailable")
	}
	if !errors.Is(err, ErrSQLiteVecUnavailable) {
		t.Fatalf("expected ErrSQLiteVecUnavailable, got %v", err)
	}
}
