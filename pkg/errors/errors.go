// Package errors provides common error types and utilities for canopy.
//
// Error Handling Guidelines:
//
// 1. WRAPPING: Always wrap errors with context using fmt.Errorf("context: %w", err).
//    This preserves the error chain for debugging while adding context.
//
// 2. SENTINEL ERRORS: Use errors.Is() to check for specific error conditions.
//    Example: if errors.Is(err, ErrOverlayNotMounted) { ... }
//
// 3. ERROR TYPES: Use errors.As() to extract typed errors for detailed handling.
//    Example: var mergeErr *MergeError; if errors.As(err, &mergeErr) { ... }
//
// 4. NEVER SWALLOW ERRORS: If an error cannot be returned, it must be logged.
//    Use "warning:" prefix for non-fatal errors in verbose output.
//
// 5. RETURN VS LOG: Return errors when the caller can handle them.
//    Log (and continue) only for truly non-fatal side effects.
package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common conditions.
// Use errors.Is() to check for these.
var (
	// ErrOverlayNotMounted indicates the overlay filesystem is not mounted.
	ErrOverlayNotMounted = errors.New("overlay not mounted")

	// ErrOverlayAlreadyMounted indicates the overlay filesystem is already mounted.
	ErrOverlayAlreadyMounted = errors.New("overlay already mounted")

	// ErrMergeConflict indicates a merge operation encountered conflicts.
	ErrMergeConflict = errors.New("merge conflict")

	// ErrTaskNotFound indicates a task ID was not found.
	ErrTaskNotFound = errors.New("task not found")

	// ErrQueueClosed indicates the merge queue has been closed.
	ErrQueueClosed = errors.New("queue closed")

	// ErrResolverTimeout indicates the resolver agent timed out.
	ErrResolverTimeout = errors.New("resolver timeout")

	// ErrResolverFailed indicates the resolver agent failed to resolve conflicts.
	ErrResolverFailed = errors.New("resolver failed")

	// ErrGitPatchFailed indicates git patch application failed.
	ErrGitPatchFailed = errors.New("git patch application failed")

	// ErrSandboxNotSupported indicates sandboxing is not supported on this platform.
	ErrSandboxNotSupported = errors.New("sandbox not supported on this platform")

	// ErrDirtyWorkingDirectory indicates the working directory has uncommitted changes
	// when it should be clean. This is a critical error that indicates merge cleanup failed.
	ErrDirtyWorkingDirectory = errors.New("dirty working directory")
)

// MergeError provides detailed information about merge failures.
type MergeError struct {
	TaskID   string   // The task that failed to merge
	Errors   []string // List of specific merge errors
	Conflict bool     // True if this was a conflict (vs other error)
}

func (e *MergeError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("merge failed for task %s", e.TaskID)
	}
	return fmt.Sprintf("merge failed for task %s: %s", e.TaskID, e.Errors[0])
}

// Unwrap returns the underlying error for error chain support.
func (e *MergeError) Unwrap() error {
	if e.Conflict {
		return ErrMergeConflict
	}
	return nil
}

// ResolverError provides detailed information about resolver failures.
type ResolverError struct {
	TaskID  string // The task the resolver was trying to fix
	Message string // Error message from the resolver
	Timeout bool   // True if the resolver timed out
}

func (e *ResolverError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("resolver timeout for task %s", e.TaskID)
	}
	return fmt.Sprintf("resolver failed for task %s: %s", e.TaskID, e.Message)
}

// Unwrap returns the underlying error for error chain support.
func (e *ResolverError) Unwrap() error {
	if e.Timeout {
		return ErrResolverTimeout
	}
	return ErrResolverFailed
}

// OverlayError provides detailed information about overlay filesystem failures.
type OverlayError struct {
	Operation string // mount, unmount, cleanup, etc.
	Path      string // The path involved
	Err       error  // The underlying error
}

func (e *OverlayError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("overlay %s failed for %s: %v", e.Operation, e.Path, e.Err)
	}
	return fmt.Sprintf("overlay %s failed for %s", e.Operation, e.Path)
}

// Unwrap returns the underlying error for error chain support.
func (e *OverlayError) Unwrap() error {
	return e.Err
}

// GitError provides detailed information about git operation failures.
type GitError struct {
	Operation string // add, commit, am, etc.
	Stderr    string // Stderr output from git
	Err       error  // The underlying error
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("git %s failed: %s", e.Operation, e.Stderr)
	}
	if e.Err != nil {
		return fmt.Sprintf("git %s failed: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("git %s failed", e.Operation)
}

// Unwrap returns the underlying error for error chain support.
func (e *GitError) Unwrap() error {
	return e.Err
}

// Wrap wraps an error with additional context.
// This is a convenience function equivalent to fmt.Errorf("%s: %w", msg, err).
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf wraps an error with formatted context.
// This is a convenience function equivalent to fmt.Errorf(format+": %w", args..., err).
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// Is reports whether any error in err's chain matches target.
// This is re-exported from the standard library for convenience.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target.
// This is re-exported from the standard library for convenience.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// New returns an error that formats as the given text.
// This is re-exported from the standard library for convenience.
func New(text string) error {
	return errors.New(text)
}
