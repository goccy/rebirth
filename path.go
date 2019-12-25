package rebirth

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ExpandPath(path string) string {
	if strings.HasPrefix(path, "./") {
		absPath, err := filepath.Abs(path)
		if err == nil {
			return absPath
		}
		return path
	}
	if strings.HasPrefix(path, "-I./") {
		absPath, err := filepath.Abs(path[2:])
		if err == nil {
			return fmt.Sprintf("-I%s", absPath)
		}
		return fmt.Sprintf("-I%s", path)
	}
	if strings.HasPrefix(path, "-L./") {
		absPath, err := filepath.Abs(path[2:])
		if err == nil {
			return fmt.Sprintf("-L%s", absPath)
		}
		return fmt.Sprintf("-L%s", path)
	}
	return path
}
