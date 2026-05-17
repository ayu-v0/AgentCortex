//go:build cgo

package sqlitevec

import sqlitevecbinding "github.com/asg017/sqlite-vec-go-bindings/cgo"

func registerSQLiteVec() error {
	sqlitevecbinding.Auto()
	return nil
}
