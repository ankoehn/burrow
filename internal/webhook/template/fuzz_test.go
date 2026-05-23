package template_test

import (
	"os"
	"path/filepath"
	"testing"

	wt "github.com/ankoehn/burrow/internal/webhook/template"
)

// knownFuzzFields is the field set used for fuzz corpus execution. It mirrors
// the ai.upstream_error payload shape so the fuzzer exercises real field
// accesses alongside free-form template source strings.
var knownFuzzFields = map[string]any{
	"ServiceID":        "svc-fuzz",
	"BackendServiceID": "be-1",
	"Status":           502,
	"Error":            "upstream timeout",
	"RetryCount":       3,
}

// FuzzTemplateRender is the sandbox fuzz harness. It must not panic,
// leak goroutines, OOM, or produce a security-relevant side effect regardless
// of the src input. The seed corpus lives in testdata/template_fuzz/ and is
// loaded from disk so new seeds can be added without recompiling.
//
// Run locally with:
//
//	go test -fuzz=FuzzTemplateRender -fuzztime=30s ./internal/webhook/template/
//
// The runtime corpus (testdata/fuzz/FuzzTemplateRender/) is gitignored;
// do NOT commit any generated crashers.
func FuzzTemplateRender(f *testing.F) {
	// Load seed corpus from testdata/template_fuzz/.
	seedDir := filepath.Join("testdata", "template_fuzz")
	entries, err := os.ReadDir(seedDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(seedDir, e.Name()))
			if err == nil {
				f.Add(string(b))
			}
		}
	}

	// Also add a few inline seeds for CI (ReadDir failure on restricted FS).
	f.Add("")
	f.Add(`{{lower .ServiceID}}`)
	f.Add(`{{exec "ls"}}`)
	f.Add(`{{ template "x" . }}`)
	f.Add("{{")
	f.Add(`{{printf "%9999999d" .Status}}`)
	f.Add(`{{b64dec "not-valid-base64!!!"}}`)

	f.Fuzz(func(t *testing.T, src string) {
		// Must not panic. Errors are fine — they are the sandbox working as
		// intended. OOM is guarded by the test runner's memory limit.
		_, _, _ = wt.Render(src, knownFuzzFields)
	})
}
