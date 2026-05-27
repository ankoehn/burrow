// Package quota — request-context helpers for quota.Subjects.
//
// The AI gateway chain injects Subjects into the request context before
// invoking the RateLimit middleware so the middleware can call Charge without
// knowing about aigw.Service directly (avoiding an import cycle).
package quota

import "context"

type ctxSubjectsKey struct{}

// WithSubjects returns a copy of ctx with who stashed under the quota key.
// Called from internal/aigw chain.go at step 3 before the RateLimit hook.
func WithSubjects(ctx context.Context, who Subjects) context.Context {
	return context.WithValue(ctx, ctxSubjectsKey{}, who)
}

// SubjectsFromCtx returns the Subjects previously stored by WithSubjects, or
// a zero Subjects when none is present (which Charge interprets as an
// unauthenticated, non-service-scoped call — only global limits apply).
func SubjectsFromCtx(ctx context.Context) Subjects {
	v, _ := ctx.Value(ctxSubjectsKey{}).(Subjects)
	return v
}
