{{- define "polars-k8s-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "polars-k8s-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
<fullname>-<suffix>, with the fullname truncated so the result fits in 63 characters.
Takes a dict with .suffix and .context (the root context).
*/}}
{{- define "polars-k8s-operator.resourceName" -}}
{{- $fullname := include "polars-k8s-operator.fullname" .context }}
{{- $maxLen := sub 62 (len .suffix) | int }}
{{- printf "%s-%s" (trunc $maxLen $fullname | trimSuffix "-") .suffix }}
{{- end }}

{{/*
Name for a cluster-scoped object. With rbac.namespaced the release namespace is
appended, so namespaced releases in different namespaces don't collide. RBAC
names aren't limited to 63 characters, so this doesn't truncate.
Takes a dict with .suffix and .context (the root context).
*/}}
{{- define "polars-k8s-operator.clusterResourceName" -}}
{{- $name := printf "%s-%s" (include "polars-k8s-operator.fullname" .context) .suffix }}
{{- if .context.Values.rbac.namespaced }}
{{- $name = printf "%s-%s" $name .context.Release.Namespace }}
{{- end }}
{{- $name }}
{{- end }}

{{- define "polars-k8s-operator.serviceAccountName" -}}
{{- default (include "polars-k8s-operator.resourceName" (dict "suffix" "controller-manager" "context" .)) .Values.serviceAccount.name }}
{{- end }}

{{- define "polars-k8s-operator.labels" -}}
app.kubernetes.io/name: {{ include "polars-k8s-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- if eq .Values.creator "helm" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end }}
{{- end }}

{{/* Deployment selectors are immutable: keep these stable across chart versions. */}}
{{- define "polars-k8s-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "polars-k8s-operator.name" . }}
control-plane: controller-manager
{{- end }}

{{/* Parses a file that `make chart` copies into files/, failing when it is missing. */}}
{{- define "polars-k8s-operator.generatedFile" -}}
{{- $content := .context.Files.Get .path }}
{{- if not $content }}
{{- fail (printf "%s is missing; run `make chart` to copy it from config/" .path) }}
{{- end }}
{{- $content }}
{{- end }}
