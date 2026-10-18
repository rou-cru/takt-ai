{{- define "takt-ai.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "takt-ai.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}{{- .Release.Name | trunc 63 | trimSuffix "-" -}}{{- else -}}{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}{{- end -}}
{{- end -}}
{{- end -}}
{{- define "takt-ai.selectorLabels" -}}
app.kubernetes.io/name: {{ include "takt-ai.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "takt-ai.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "takt-ai.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}{{ toYaml . }}{{- end }}
{{- end -}}
{{- define "takt-ai.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}{{- default (printf "sa-%s" (include "takt-ai.fullname" .)) .Values.serviceAccount.name -}}{{- else -}}{{- default "default" .Values.serviceAccount.name -}}{{- end -}}
{{- end -}}
{{/* Released charts carry appVersion == image tag, so the default needs no values. */}}
{{- define "takt-ai.image" -}}
{{- $repository := required "image.repository must be set to a published workspace image" .Values.image.repository -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%v" $repository (required "image.tag, image.digest or the chart appVersion must name a published workspace image" (default .Chart.AppVersion .Values.image.tag)) -}}
{{- end -}}
{{- end -}}
{{/* Rejects duplicate names in the coder.coder.env list the Coder control plane
     consumes; a duplicate would silently replace an earlier entry, including a
     required one. Renders nothing when names are unique; fails naming the repeated
     name. Invoked from templates/NOTES.txt, which renders wherever the Coder
     dependency renders. */}}
{{- define "takt-ai.coderEnvGuard" -}}
{{- $seen := dict -}}
{{- range .Values.coder.coder.env -}}
{{- if hasKey $seen .name -}}
{{- fail (printf "duplicate Coder env name %q in coder.coder.env; env names must be unique" .name) -}}
{{- end -}}
{{- $_ := set $seen .name true -}}
{{- end -}}
{{- end -}}
