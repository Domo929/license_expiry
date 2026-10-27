// Package errs defines the error kinds that clients can cause.
//
// Business code wraps one kind when a client causes the error:
//
//	return fmt.Errorf("user %d: %w", id, errs.ErrNotFound)
//
// The app layer maps each kind to a response, such as an HTTP status code.
// The app layer sends the text of these errors to clients.
// Never wrap an internal error, such as a database error, into one of these kinds.
package errs

import "errors"

// The error kinds, in the order that the app layer checks them.
var (
	// ErrInvalid means that the input breaks a format or meaning rule.
	ErrInvalid = errors.New("invalid")

	// ErrUnauthenticated means that the caller did not prove its identity.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden means that the caller must not do the operation.
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound means that the requested entity does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict means that the operation conflicts with the current state, such as a duplicate key.
	ErrConflict = errors.New("conflict")
)
