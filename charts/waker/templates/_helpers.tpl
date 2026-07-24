{{- define "waker.labels" -}}
app.kubernetes.io/name: waker
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: {{ .Values.global.project }}
{{- end }}

{{- define "waker.selectorLabels" -}}
app.kubernetes.io/name: waker
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
