{{/*
Expand the name of the chart.
*/}}
{{- define "name" -}}
gardener-extension-provider-stackit
{{- end -}}

{{- define "chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "labels" -}}
helm.sh/chart: {{ include "chart" . }}
{{ include "selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "selectorLabels" -}}
app.kubernetes.io/name: {{ include "name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{-  define "image" -}}
  {{- if .Values.image.ref }}
  {{- printf "%s" .Values.image.ref }}
  {{- else if hasPrefix "sha256:" .Values.image.tag }}
  {{- printf "%s@%s" .Values.image.repository .Values.image.tag }}
  {{- else }}
  {{- printf "%s:%s" .Values.image.repository .Values.image.tag }}
  {{- end }}
{{- end }}

{{- define "deploymentversion" -}}
apps/v1
{{- end -}}

{{- define "runtimeCluster.enabled" -}}
{{- /* TODO: Remove gardener.runtimeCluster.enabled, replaced by gardener.runtimeCluster, it will be no longer supported by Gardener after v1.159.0 is released. */}}
{{- if (or .Values.gardener.runtimeCluster (.Values | merge (dict) | dig "gardener" "runtimeCluster" "enabled" false)) }}
true
{{- end }}
{{- end -}}


{{- define "featureGates" -}}
  {{- $gates := list -}}
  {{- range $key, $val := . }}
  {{- $gate := printf "%s=%s" $key ($val | toString) -}}
  {{- $gates = $gate | append $gates -}}
  {{- end -}}
  {{- join "," $gates }}
{{- end -}}
