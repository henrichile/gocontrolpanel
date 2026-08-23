# Caddy de borde con WAF (Coraza + OWASP CRS, equivalente a mod_security) y
# rate limiting propios, compilados con xcaddy porque no son parte del build
# oficial de Caddy. Se activa poniendo GOCP_WAF_ENABLED=true en el panel —
# sin esa variable, el panel nunca manda un handler "coraza_waf"/"rate_limit"
# en la config, así que esta imagen también funciona igual que la oficial.

FROM caddy:2-builder-alpine AS builder

RUN xcaddy build \
    --with github.com/corazawaf/coraza-caddy/v2 \
    --with github.com/mholt/caddy-ratelimit

FROM caddy:2-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

# OWASP Core Rule Set, versión fija (no "latest"): las reglas de Coraza en
# GOCP_CORAZA_DIRECTIVES (internal/config/config.go) apuntan por defecto a
# /etc/coraza-crs/{crs-setup.conf,rules/*.conf}.
ARG CRS_VERSION=4.9.0
RUN apk add --no-cache curl tar && \
    curl -fsSL "https://github.com/coreruleset/coreruleset/archive/refs/tags/v${CRS_VERSION}.tar.gz" \
        -o /tmp/crs.tar.gz && \
    tar xzf /tmp/crs.tar.gz -C /etc && \
    mv "/etc/coreruleset-${CRS_VERSION}" /etc/coraza-crs && \
    cp /etc/coraza-crs/crs-setup.conf.example /etc/coraza-crs/crs-setup.conf && \
    rm /tmp/crs.tar.gz && \
    apk del curl tar
