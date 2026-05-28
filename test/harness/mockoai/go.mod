// test-only — never deploy this shape.
// Nested module: keeps mockoai stdlib-only and Docker-buildable from its
// own directory context (test/harness/mockoai), independent of
// the parent burrow module.
module mockoai

go 1.26
