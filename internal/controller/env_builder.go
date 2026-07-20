package controller

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

// envBuilder accumulates corev1.EnvVar entries under a "__"-joined name, so
// deeply nested keys like "PC_CUBLET__scheduler__checkpoint__period" are
// built by chaining Section calls instead of hand-typing the full string.
// All builders derived from the same root (via Section) share one
// underlying, order-preserving slice.
type envBuilder struct {
	prefix string
	vars   *[]corev1.EnvVar
}

// newEnvBuilder returns a root builder with no prefix. Calls on the root add
// bare names (e.g. "RUST_BACKTRACE"); use Section to add a prefix.
func newEnvBuilder() *envBuilder {
	var vars []corev1.EnvVar
	return &envBuilder{vars: &vars}
}

// Section returns a builder scoped to name, joined onto the current prefix
// with "__".
func (b *envBuilder) Section(name string) *envBuilder {
	return &envBuilder{prefix: b.name(name), vars: b.vars}
}

func (b *envBuilder) name(leaf string) string {
	if b.prefix == "" {
		return leaf
	}
	return b.prefix + "__" + leaf
}

func (b *envBuilder) String(name, value string) {
	*b.vars = append(*b.vars, corev1.EnvVar{Name: b.name(name), Value: value})
}

func (b *envBuilder) Bool(name string, value bool) {
	b.String(name, strconv.FormatBool(value))
}

func (b *envBuilder) Int(name string, value int64) {
	b.String(name, strconv.FormatInt(value, 10))
}

func (b *envBuilder) Duration(name string, value time.Duration) {
	b.String(name, value.String())
}

func (b *envBuilder) From(name string, source *corev1.EnvVarSource) {
	*b.vars = append(*b.vars, corev1.EnvVar{Name: b.name(name), ValueFrom: source})
}

// FieldRef sets name from a downward-API field, e.g. FieldRef("instance_id", "metadata.uid").
func (b *envBuilder) FieldRef(name, fieldPath string) {
	b.From(name, &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: fieldPath}})
}

func (b *envBuilder) ValueOrSource(name string, vs computev1.ValueOrSource) {
	if vs.ValueFrom != nil {
		b.From(name, vs.ValueFrom)
		return
	}
	b.String(name, vs.Value)
}

// Options appends each option under the current prefix, preserving its own
// Value/ValueFrom, e.g. an "aws_access_key_id" option on a builder scoped to
// "...__s3" becomes "...__s3__aws_access_key_id".
func (b *envBuilder) Options(options []corev1.EnvVar) {
	for _, option := range options {
		*b.vars = append(*b.vars, corev1.EnvVar{
			Name:      b.name(option.Name),
			Value:     option.Value,
			ValueFrom: option.ValueFrom,
		})
	}
}

// Vars returns the accumulated env vars in call order.
func (b *envBuilder) Vars() []corev1.EnvVar {
	return *b.vars
}
