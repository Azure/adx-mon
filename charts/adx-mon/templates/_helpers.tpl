{{/*
Resolve a component image reference. Usage: {{ include "adx-mon.image" (dict "image" .Values.collector.image "component" "collector" "registry" .Values.image_registry) }}
*/}}
{{- define "adx-mon.image" -}}
{{- $image := .image -}}
{{- $registry := .registry -}}
{{- $repo := $image.repository | default (printf "%s/%s" $registry .component) -}}
{{- $tag := $image.tag | default "latest" -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
