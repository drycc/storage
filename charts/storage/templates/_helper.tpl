{{- /* keep randAlphaNum values consistent */ -}}
{{- define "storage.accesskey" -}}
  {{- if not (index .Release "secrets") -}}
    {{- $_ := set .Release "secrets" dict -}}
  {{- end -}}
  {{- if not (index .Release.secrets "accesskey") -}}
    {{- if .Values.accesskey | default "" | ne "" -}}
      {{- $_ := set .Release.secrets "accesskey" .Values.accesskey -}}
    {{- else -}}
      {{- $_ := set .Release.secrets "accesskey" (randAlphaNum 32) -}}
    {{- end -}}
  {{- end -}}
  {{- index .Release.secrets "accesskey" -}}
{{- end -}}

{{- /* keep randAlphaNum values consistent */ -}}
{{- define "storage.secretkey" -}}
  {{- if not (index .Release "secrets") -}}
    {{- $_ := set .Release "secrets" dict -}}
  {{- end -}}
  {{- if not (index .Release.secrets "secretkey") -}}
    {{- if .Values.secretkey | default "" | ne "" -}}
      {{- $_ := set .Release.secrets "secretkey" .Values.secretkey -}}
    {{- else -}}
      {{- $_ := set .Release.secrets "secretkey" (randAlphaNum 32) -}}
    {{- end -}}
  {{- end -}}
  {{- index .Release.secrets "secretkey" -}}
{{- end -}}
