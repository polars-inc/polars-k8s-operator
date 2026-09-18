package controller

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// specError marks an error caused by the cluster's spec (an invalid
// podTemplate, an API-server-rejected computed object) rather than an
// unexpected/infrastructure failure. Reconcile surfaces it as a status
// condition + Warning Event and returns nil, instead of the default noisy
// requeue-with-backoff.
type specError struct {
	reason string
	err    error
}

func (e *specError) Error() string { return e.err.Error() }
func (e *specError) Unwrap() error { return e.err }

// specErrorf builds a spec error raised directly by the operator's own
// validation (e.g. a podTemplate missing required containers).
func specErrorf(reason, format string, a ...any) *specError {
	return &specError{reason: reason, err: fmt.Errorf(format, a...)}
}

// classifyAPIError reclassifies err as a spec error when the API server
// rejected the request because of the object's content (Invalid/Forbidden —
// i.e. the user's spec), leaving every other error (conflict, timeout, not
// found, etc.) unchanged.
func classifyAPIError(reason string, err error) error {
	if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) {
		return &specError{reason: reason, err: err}
	}
	return err
}

func reducePodErrors(errs []error) (setAsideSpecErrs []error, err error) {
	var transient, spec []error
	for _, e := range errs {
		if e == nil {
			continue
		}
		var se *specError
		if errors.As(e, &se) {
			spec = append(spec, e)
			continue
		}
		transient = append(transient, e)
	}

	switch {
	case len(transient) > 0:
		return spec, errors.Join(transient...)
	case len(spec) > 0:
		var first *specError
		_ = errors.As(spec[0], &first)
		return nil, &specError{reason: first.reason, err: errors.Join(spec...)}
	default:
		return nil, nil
	}
}
