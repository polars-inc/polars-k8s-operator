package controller

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// mergePodSpec strategic-merges patch onto base, using corev1.PodSpec's
// patchMergeKey/patchStrategy struct tags (Containers/Volumes by name, Env by
// name, VolumeMounts by mountPath, ImagePullSecrets by name) so lists merge
// additively instead of being replaced or duplicated across reconciles.
func mergePodSpec(base, patch corev1.PodSpec) (corev1.PodSpec, error) {
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	mergedJSON, err := strategicpatch.StrategicMergePatch(baseJSON, patchJSON, corev1.PodSpec{})
	if err != nil {
		return corev1.PodSpec{}, err
	}

	var merged corev1.PodSpec
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return corev1.PodSpec{}, err
	}

	return merged, nil
}
