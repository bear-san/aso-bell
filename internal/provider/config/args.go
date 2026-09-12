package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

const envPrefix = "ASOBELL_"

// applyArgs は --manager-addr=... 形式の引数を環境変数名に変換して environ に上書きする。
// 引数が環境変数より優先されるという docs/12 §2 の規則を、ここで先に environ を書き換えることで実現する。
// --xxx-file は指定ファイルの内容を値として使い、トークンをプロセス一覧へ露出させずに済ませる。
func applyArgs(environ map[string]string, args []string, allowed map[string]struct{}) error {
	for i := 0; i < len(args); i++ {
		key, value, consumed, err := splitArg(args, i)
		if err != nil {
			return err
		}
		i += consumed
		name, fromFile, err := resolveFlag(key, allowed)
		if err != nil {
			return err
		}
		if fromFile {
			b, readErr := os.ReadFile(value)
			if readErr != nil {
				return fmt.Errorf("read --%s: %w", key, readErr)
			}
			value = strings.TrimSpace(string(b))
		}
		environ[name] = value
	}
	return nil
}

// splitArg は args[i] を --key=value または --key value として解釈し、
// キー、値、追加で消費した要素数を返す。
func splitArg(args []string, i int) (string, string, int, error) {
	arg := args[i]
	if !strings.HasPrefix(arg, "--") {
		return "", "", 0, fmt.Errorf("unexpected argument %q", arg)
	}
	key, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	if key == "" {
		return "", "", 0, fmt.Errorf("unexpected argument %q", arg)
	}
	if hasValue {
		return key, value, 0, nil
	}
	if i+1 >= len(args) {
		return "", "", 0, fmt.Errorf("flag --%s requires a value", key)
	}
	return key, args[i+1], 1, nil
}

// resolveFlag はフラグ名を環境変数名に対応付け、ファイル指定かどうかも返す。
// -file 付きは、その名前自体が環境変数として存在しない場合にだけファイル指定として扱う。
func resolveFlag(key string, allowed map[string]struct{}) (string, bool, error) {
	name := flagToEnv(key)
	if _, ok := allowed[name]; ok {
		return name, false, nil
	}
	if base, isFile := strings.CutSuffix(key, "-file"); isFile {
		if _, ok := allowed[flagToEnv(base)]; ok {
			return flagToEnv(base), true, nil
		}
	}
	return "", false, fmt.Errorf("unknown flag --%s", key)
}

func flagToEnv(flag string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// envNames は構造体(埋め込み・ネストを含む)の env タグ名を集める。
func envNames(t reflect.Type, out map[string]struct{}) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag, tagged := f.Tag.Lookup("env")
		if !tagged && f.Type.Kind() == reflect.Struct {
			envNames(f.Type, out)
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			out[name] = struct{}{}
		}
	}
}
