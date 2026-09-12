// Package version は各バイナリが報告するビルド情報を保持する。
package version

import (
	"fmt"
	"runtime"
)

// Version はビルド時に -ldflags "-X .../version.Version=..." で差し替えられる。
// 既定値 dev は go run や未指定ビルドを識別するために残す。
var Version = "dev"

// String は name と Version、Go ランタイム情報を 1 行にまとめる。
func String(name string) string {
	return fmt.Sprintf("%s %s (%s %s/%s)", name, Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
