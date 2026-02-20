{{- define "stackit-cloud-controller-manager.featureGates" -}}
{{- if .Values.featureGates }}
- --feature-gates={{ range $feature, $enabled := .Values.featureGates }}{{ $feature }}={{ $enabled }},{{ end }}
{{- end }}
{{- end -}}

{{- define "stackit-cloud-controller-manager.controllers" -}}
{{- if .Values.controllers }}
- --controllers={{ range $controller := .Values.controllers }}{{ $controller }},{{ end }}
{{- end }}
{{- end -}}
