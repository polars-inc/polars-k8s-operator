package controller

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassifyAPIError(t *testing.T) {
	gr := schema.GroupResource{Group: "", Resource: "pods"}

	tests := map[string]struct {
		err       error
		wantsSpec bool
	}{
		"invalid": {
			err:       apierrors.NewInvalid(schema.GroupKind{Group: "", Kind: "Pod"}, "test", nil),
			wantsSpec: true,
		},
		"forbidden": {
			err:       apierrors.NewForbidden(gr, "test", errors.New("denied")),
			wantsSpec: true,
		},
		"conflict": {
			err:       apierrors.NewConflict(gr, "test", errors.New("conflict")),
			wantsSpec: false,
		},
		"generic": {
			err:       errors.New("boom"),
			wantsSpec: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := classifyAPIError("SomeReason", tt.err)

			var se *specError
			isSpec := errors.As(got, &se)
			if isSpec != tt.wantsSpec {
				t.Fatalf("classifyAPIError(%v) spec-error = %v, want %v", tt.err, isSpec, tt.wantsSpec)
			}
			if isSpec {
				if se.reason != "SomeReason" {
					t.Fatalf("expected reason %q, got %q", "SomeReason", se.reason)
				}
				if !errors.Is(got, tt.err) {
					t.Fatalf("expected classifyAPIError to wrap the original error")
				}
			} else if got != tt.err {
				t.Fatalf("expected classifyAPIError to pass the error through unchanged")
			}
		})
	}
}
