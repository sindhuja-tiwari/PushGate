package registry

import (
	"path/filepath"
	"strings"
)

// PanicSummary picks the useful lines out of a crash. Rust prints
// "thread 'x' panicked at file:line:col:" then the message. Go prints
// "panic: msg" and later "\tfile.go:line".
func PanicSummary(tail []string) string {
	for i, l := range tail {
		if strings.Contains(l, "panicked at") {
			msg := strings.TrimSpace(l)
			if i+1 < len(tail) {
				msg += " " + strings.TrimSpace(tail[i+1])
			}
			return msg
		}
		if strings.HasPrefix(l, "panic: ") {
			msg := strings.TrimSpace(l)
			// The first stack frame outside the Go runtime is the culprit.
			for _, m := range tail[i+1:] {
				if !strings.HasPrefix(m, "\t") || !strings.Contains(m, ".go:") || strings.Contains(m, "/runtime/") {
					continue
				}
				msg += " at " + filepath.Base(strings.Fields(m)[0])
				break
			}
			return msg
		}
	}
	return ""
}

