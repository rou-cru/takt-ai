{{- define "takt-workspace.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "takt-workspace.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}{{- .Release.Name | trunc 63 | trimSuffix "-" -}}{{- else -}}{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}{{- end -}}
{{- end -}}
{{- end -}}
{{- define "takt-workspace.selectorLabels" -}}
app.kubernetes.io/name: {{ include "takt-workspace.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "takt-workspace.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "takt-workspace.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}{{ toYaml . }}{{- end }}
{{- end -}}
{{- define "takt-workspace.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}{{- default (printf "sa-%s" (include "takt-workspace.fullname" .)) .Values.serviceAccount.name -}}{{- else -}}{{- default "default" .Values.serviceAccount.name -}}{{- end -}}
{{- end -}}
{{- define "takt-workspace.image" -}}
{{- $repository := required "image.repository must be set to a published workspace image" .Values.image.repository -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%v" $repository (required "image.tag or image.digest must be set to a published workspace image" .Values.image.tag) -}}
{{- end -}}
{{- end -}}
