package render

import (
	"fmt"
	"time"
)

// Step wraps a unit of work with scoped house logging + pretty start/finish:
// an INFO "action…" line, then ✓ "action (dur)" on success or ✗ "action: err"
// on failure — each rendered to the terminal AND appended to framework.log.
// Generic in the result type; domain-free, so it travels with any houseui reuse.
func Step[T any](scope, action string, fn func() (T, error)) (T, error) {
	log := Scoped(scope)
	log.Info(action + " …")
	start := time.Now()
	out, err := fn()
	if err != nil {
		log.Err(fmt.Sprintf("%s: %v", action, err))
		return out, err
	}
	log.OK(fmt.Sprintf("%s (%s)", action, time.Since(start).Round(time.Millisecond)))
	return out, nil
}

// Do is the no-result variant of Step.
func Do(scope, action string, fn func() error) error {
	_, err := Step(scope, action, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
