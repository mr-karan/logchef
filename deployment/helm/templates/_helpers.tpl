{{- define "logchef.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "logchef.fullname" -}}
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

{{- define "logchef.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "logchef.labels" -}}
helm.sh/chart: {{ include "logchef.chart" . }}
{{ include "logchef.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "logchef.selectorLabels" -}}
app.kubernetes.io/name: {{ include "logchef.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "logchef.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "logchef.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "logchef.logchefServiceName" -}}
{{- include "logchef.fullname" . }}
{{- end }}

{{- define "logchef.apiTokenSecretName" -}}
{{- if .Values.logchef.auth.existingSecret }}
{{- .Values.logchef.auth.existingSecret }}
{{- else }}
{{- printf "%s-api-token" (include "logchef.logchefServiceName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "logchef.apiTokenSecretKey" -}}
{{- .Values.logchef.auth.apiTokenSecretKey | default "api-token-secret" }}
{{- end }}

{{- /* Reuse the in-cluster Secret when present (helm upgrade). Generate openssl rand -hex 32 on first install. */ -}}
{{- define "logchef.apiTokenSecretValue" -}}
{{- $name := printf "%s-api-token" (include "logchef.logchefServiceName" .) | trunc 63 | trimSuffix "-" }}
{{- $key := include "logchef.apiTokenSecretKey" . }}
{{- $existing := lookup "v1" "Secret" .Release.Namespace $name }}
{{- if and $existing $existing.data (index $existing.data $key) }}
{{- index $existing.data $key | b64dec }}
{{- else if .Values.logchef.config.auth.api_token_secret }}
{{- .Values.logchef.config.auth.api_token_secret }}
{{- else }}
{{- printf "%x" (randBytes 32 | b64dec) }}
{{- end }}
{{- end }}

{{- define "logchef.dexServiceName" -}}
{{- printf "%s-dex" (include "logchef.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "logchef.publicDomain" -}}
{{- .Values.publicDomain | default "" -}}
{{- end }}

{{- define "logchef.dexInternalURL" -}}
{{- printf "http://%s:%v" (include "logchef.dexServiceName" .) .Values.dex.service.port -}}
{{- end }}

{{- define "logchef.dexIssuer" -}}
{{- $domain := include "logchef.publicDomain" . -}}
{{- if $domain -}}
{{- printf "https://%s.%s/dex" (include "logchef.dexServiceName" .) $domain -}}
{{- else -}}
{{- printf "%s/dex" (include "logchef.dexInternalURL" .) -}}
{{- end -}}
{{- end }}

{{- define "logchef.logchefPublicURL" -}}
{{- $domain := include "logchef.publicDomain" . -}}
{{- if $domain -}}
{{- printf "https://%s.%s" (include "logchef.logchefServiceName" .) $domain -}}
{{- else -}}
{{- printf "http://%s:%v" (include "logchef.logchefServiceName" .) .Values.logchef.service.port -}}
{{- end -}}
{{- end }}

{{- define "logchef.oidcProviderURL" -}}
{{- /* Must match Dex issuer exactly (go-oidc discovery). When publicDomain is set, Dex issuer is https://…/dex — not the in-cluster http URL. */ -}}
{{- $domain := include "logchef.publicDomain" . -}}
{{- if .Values.logchef.config.oidc.provider_url -}}
{{- .Values.logchef.config.oidc.provider_url -}}
{{- else if $domain -}}
{{- printf "https://%s.%s/dex" (include "logchef.dexServiceName" .) $domain -}}
{{- else -}}
{{- printf "%s/dex" (include "logchef.dexInternalURL" .) -}}
{{- end -}}
{{- end }}

{{- define "logchef.oidcAuthURL" -}}
{{- $domain := include "logchef.publicDomain" . -}}
{{- if .Values.logchef.config.oidc.auth_url -}}
{{- .Values.logchef.config.oidc.auth_url -}}
{{- else if $domain -}}
{{- printf "https://%s.%s/dex/auth" (include "logchef.dexServiceName" .) $domain -}}
{{- else -}}
{{- printf "%s/dex/auth" (include "logchef.dexInternalURL" .) -}}
{{- end -}}
{{- end }}

{{- define "logchef.oidcTokenURL" -}}
{{- /* Server-side token exchange can stay in-cluster even when provider_url is public. */ -}}
{{- .Values.logchef.config.oidc.token_url | default (printf "%s/dex/token" (include "logchef.dexInternalURL" .)) -}}
{{- end }}

{{- define "logchef.oidcRedirectURL" -}}
{{- .Values.logchef.config.oidc.redirect_url | default (printf "%s/api/v1/auth/callback" (include "logchef.logchefPublicURL" .)) -}}
{{- end }}

{{- define "logchef.clickhouseServiceName" -}}
{{- if .Values.clickhouse.serviceName -}}
{{- .Values.clickhouse.serviceName -}}
{{- else -}}
{{- printf "clickhouse-%s" .Values.clickhouse.name -}}
{{- end -}}
{{- end }}
