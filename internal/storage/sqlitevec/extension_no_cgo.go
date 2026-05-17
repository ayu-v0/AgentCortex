//go:build !cgo

package sqlitevec

func registerSQLiteVec() error {
	return ErrSQLiteVecUnavailable
}
