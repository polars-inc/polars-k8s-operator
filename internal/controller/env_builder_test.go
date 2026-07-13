package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	computev1 "polars-inc/k8s-operator/api/v1"
)

func TestEnvBuilder_SectionJoining(t *testing.T) {
	g := NewWithT(t)

	b := newEnvBuilder()
	b.String("RUST_BACKTRACE", "full")

	cublet := b.Section("PC_CUBLET")
	cublet.Section("scheduler").Bool("enabled", true)
	cublet.Section("scheduler").Section("checkpoint").String("period", "20m")

	vars := b.Vars()
	g.Expect(vars).To(Equal([]corev1.EnvVar{
		{Name: "RUST_BACKTRACE", Value: "full"},
		{Name: "PC_CUBLET__scheduler__enabled", Value: envValueTrue},
		{Name: "PC_CUBLET__scheduler__checkpoint__period", Value: "20m"},
	}))
}

func TestEnvBuilder_FieldRefAndValueOrSource(t *testing.T) {
	g := NewWithT(t)

	b := newEnvBuilder()
	b.FieldRef("instance_id", "metadata.uid")
	b.ValueOrSource("literal", computev1.ValueOrSource{Value: "x"})
	b.ValueOrSource("sourced", computev1.ValueOrSource{
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "k"}},
	})

	vars := b.Vars()
	g.Expect(vars[0].ValueFrom.FieldRef.FieldPath).To(Equal("metadata.uid"))
	g.Expect(vars[1].Value).To(Equal("x"))
	g.Expect(vars[1].ValueFrom).To(BeNil())
	g.Expect(vars[2].ValueFrom.SecretKeyRef.Key).To(Equal("k"))
}

func TestEnvBuilder_Options(t *testing.T) {
	g := NewWithT(t)

	b := newEnvBuilder()
	s3 := b.Section("PC_CUBLET").Section("s3")
	s3.String("url", "s3://bucket")
	s3.Options([]corev1.EnvVar{{Name: testOptionRegion, Value: testOptionRegionValue}})

	vars := b.Vars()
	g.Expect(vars).To(Equal([]corev1.EnvVar{
		{Name: "PC_CUBLET__s3__url", Value: "s3://bucket"},
		{Name: "PC_CUBLET__s3__region", Value: testOptionRegionValue},
	}))
}
