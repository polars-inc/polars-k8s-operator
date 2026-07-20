package controller

import (
	"testing"

	. "github.com/onsi/gomega"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

const (
	testExistingServiceAccountName = "existing-sa"
	testCreateDefaultName          = "cluster-scheduler"
)

func TestResolveServiceAccountName(t *testing.T) {
	cases := []struct {
		name        string
		spec        *computev1.ServiceAccountSpec
		defaultName string
		want        string
	}{
		{"nil spec defaults to \"default\"", nil, testCreateDefaultName, defaultServiceAccountName},
		{"reuse with empty name defaults to \"default\"", &computev1.ServiceAccountSpec{Create: false}, testCreateDefaultName, defaultServiceAccountName},
		{"reuse with explicit name", &computev1.ServiceAccountSpec{Create: false, Name: testExistingServiceAccountName}, testCreateDefaultName, testExistingServiceAccountName},
		{"create with empty name uses defaultName", &computev1.ServiceAccountSpec{Create: true}, testCreateDefaultName, testCreateDefaultName},
		{"create with explicit name overrides defaultName", &computev1.ServiceAccountSpec{Create: true, Name: "custom-sa"}, testCreateDefaultName, "custom-sa"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(resolveServiceAccountName(tc.spec, tc.defaultName)).To(Equal(tc.want))
		})
	}
}
