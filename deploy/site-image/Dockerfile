# Imagen base de los sitios de hosting.
#
# Construye una variante por versión de PHP:
#   docker build --build-arg PHP_VERSION=8.4 -t gocp/frankenphp:php8.4 .
#   docker build --build-arg PHP_VERSION=8.3 -t gocp/frankenphp:php8.3 .
#
# El panel arranca un contenedor de esta imagen por sitio, monta el directorio
# del cliente en /app y expone el 8080 solo dentro de la red interna: el TLS y
# el enrutado los hace el Caddy de borde.

ARG PHP_VERSION=8.4
FROM dunglas/frankenphp:1-php${PHP_VERSION}

# git + openssh-client: los usa el deploy automático por Git del panel
# (clonar/actualizar el repo del sitio con su propia deploy key).
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Extensiones habituales en hosting compartido (WordPress, Laravel, Magento…).
RUN install-php-extensions \
        bcmath \
        exif \
        gd \
        intl \
        opcache \
        pcntl \
        pdo_mysql \
        pdo_pgsql \
        redis \
        soap \
        zip

# Composer, disponible para los clientes vía la consola del panel.
COPY --from=composer/composer:2-bin /composer /usr/bin/composer

# Ajustes de producción: OPcache activo, sin exponer la versión de PHP y con
# límites razonables para un entorno multi-tenant.
COPY php.ini /usr/local/etc/php/conf.d/zz-gocp.ini

# Caddy dentro del contenedor sirve HTTP plano en :8080; el borde pone el TLS.
ENV SERVER_NAME=":8080" \
    FRANKENPHP_NO_COMPRESS=0

WORKDIR /app

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/ >/dev/null || exit 1

EXPOSE 8080
