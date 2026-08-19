#!/usr/bin/env bash
#
# GoControlPanel — instalador automático
#
#   Interactivo (dentro del repositorio):
#       sudo ./install.sh
#
#   Interactivo (servidor limpio):
#       curl -fsSL https://ejemplo.com/install.sh | sudo bash
#
#   Desatendido:
#       sudo ./install.sh --unattended \
#            --domain panel.midominio.cl \
#            --email admin@midominio.cl \
#            --admin-password 'una-clave-larga'
#
# Todas las opciones se pueden pasar también por variables de entorno con el
# prefijo GOCP_ (ver --help).
 
set -Eeuo pipefail
 
# ──────────────────────────────────────────────────────────────────────────
# Valores por defecto
# ──────────────────────────────────────────────────────────────────────────
 
GOCP_REPO_URL="${GOCP_REPO_URL:-https://github.com/etasoft/gocontrolpanel.git}"
GOCP_REPO_REF="${GOCP_REPO_REF:-main}"
GOCP_INSTALL_DIR="${GOCP_INSTALL_DIR:-/opt/gocontrolpanel}"
GOCP_DATA_DIR="${GOCP_DATA_DIR:-/srv/gocp/accounts}"
GOCP_PHP_VERSIONS="${GOCP_PHP_VERSIONS:-8.3 8.4}"
 
GOCP_DOMAIN="${GOCP_DOMAIN:-}"
GOCP_EMAIL="${GOCP_EMAIL:-}"
GOCP_ADMIN_USER="${GOCP_ADMIN_USER:-admin}"
GOCP_ADMIN_PASSWORD="${GOCP_ADMIN_PASSWORD:-}"
 
UNATTENDED=0
SKIP_DOCKER_INSTALL=0
SKIP_IMAGES=0
DRY_RUN=0
NO_TLS=0
 
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo /tmp)"
LOG_FILE="/var/log/gocontrolpanel-install.log"
GENERATED_PASSWORD=""
 
# ──────────────────────────────────────────────────────────────────────────
# Presentación
# ──────────────────────────────────────────────────────────────────────────
 
if [[ -t 1 ]] && [[ "${NO_COLOR:-}" == "" ]]; then
    C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'
    C_BLUE=$'\033[34m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'
else
    C_RESET=""; C_BOLD=""; C_DIM=""; C_BLUE=""; C_GREEN=""; C_YELLOW=""; C_RED=""
fi
 
log()   { printf '%s\n' "$*" | tee -a "$LOG_FILE" >/dev/null 2>&1 || true; }
info()  { printf '%s\n' "  $*"; log "INFO  $*"; }
paso()  { printf '\n%s▸ %s%s\n' "$C_BOLD$C_BLUE" "$*" "$C_RESET"; log "PASO  $*"; }
ok()    { printf '%s  ✓%s %s\n' "$C_GREEN" "$C_RESET" "$*"; log "OK    $*"; }
aviso() { printf '%s  ! %s%s\n' "$C_YELLOW" "$*" "$C_RESET"; log "AVISO $*"; }
error() { printf '%s  ✗ %s%s\n' "$C_RED" "$*" "$C_RESET" >&2; log "ERROR $*"; }
 
morir() {
    error "$1"
    [[ -n "${2:-}" ]] && printf '\n    %s\n' "$2" >&2
    printf '\n  El registro completo está en %s\n\n' "$LOG_FILE" >&2
    exit 1
}
 
banner() {
    printf '\n%s' "$C_BOLD"
    cat <<'EOF'
   ____       ____            _             _ ____                  _
  / ___| ___ / ___|___  _ __ | |_ _ __ ___ | |  _ \ __ _ _ __   ___| |
 | |  _ / _ \ |   / _ \| '_ \| __| '__/ _ \| | |_) / _` | '_ \ / _ \ |
 | |_| | (_) | |__| (_) | | | | |_| | | (_) | |  __/ (_| | | | |  __/ |
  \____|\___/\____\___/|_| |_|\__|_|  \___/|_|_|   \__,_|_| |_|\___|_|
EOF
    printf '%s' "$C_RESET"
    printf '%s  Panel de hosting sobre FrankenPHP y Caddy%s\n\n' "$C_DIM" "$C_RESET"
}
 
trap 'error "El instalador falló en la línea $LINENO. Revisa $LOG_FILE"' ERR
 
# ──────────────────────────────────────────────────────────────────────────
# Argumentos
# ──────────────────────────────────────────────────────────────────────────
 
uso() {
    cat <<EOF
${C_BOLD}GoControlPanel — instalador${C_RESET}
 
  sudo ./install.sh [opciones]
 
${C_BOLD}Opciones${C_RESET}
  --domain DOMINIO         Dominio del panel, p. ej. panel.midominio.cl
  --email CORREO           Correo de contacto para Let's Encrypt
  --admin-user USUARIO     Usuario administrador (por defecto: admin)
  --admin-password CLAVE   Contraseña del administrador (mínimo 10 caracteres)
  --install-dir RUTA       Dónde instalar (por defecto: $GOCP_INSTALL_DIR)
  --data-dir RUTA          Archivos de los clientes (por defecto: $GOCP_DATA_DIR)
  --php-versions "8.3 8.4" Versiones de PHP a construir
  --repo-url URL           Repositorio git si no se ejecuta dentro del proyecto
  --repo-ref REF           Rama o etiqueta a usar (por defecto: $GOCP_REPO_REF)
 
  --unattended             No preguntar nada; falla si falta algún dato obligatorio
  --skip-docker-install    No instalar Docker aunque falte; solo comprobar
  --skip-images            No construir las imágenes de PHP (más rápido para pruebas)
  --no-tls                 Instalación local sin certificados (desarrollo)
  --dry-run                Mostrar lo que se haría, sin tocar el sistema
  -h, --help               Esta ayuda
 
${C_BOLD}Variables de entorno equivalentes${C_RESET}
  GOCP_DOMAIN, GOCP_EMAIL, GOCP_ADMIN_USER, GOCP_ADMIN_PASSWORD,
  GOCP_INSTALL_DIR, GOCP_DATA_DIR, GOCP_PHP_VERSIONS, GOCP_REPO_URL, GOCP_REPO_REF
 
${C_BOLD}Ejemplo desatendido${C_RESET}
  sudo ./install.sh --unattended --domain panel.midominio.cl \\
       --email admin@midominio.cl --admin-password 'clave-muy-larga'
EOF
}
 
while [[ $# -gt 0 ]]; do
    case "$1" in
        --domain)          GOCP_DOMAIN="${2:?falta el valor de --domain}"; shift 2 ;;
        --email)           GOCP_EMAIL="${2:?falta el valor de --email}"; shift 2 ;;
        --admin-user)      GOCP_ADMIN_USER="${2:?falta el valor}"; shift 2 ;;
        --admin-password)  GOCP_ADMIN_PASSWORD="${2:?falta el valor}"; shift 2 ;;
        --install-dir)     GOCP_INSTALL_DIR="${2:?falta el valor}"; shift 2 ;;
        --data-dir)        GOCP_DATA_DIR="${2:?falta el valor}"; shift 2 ;;
        --php-versions)    GOCP_PHP_VERSIONS="${2:?falta el valor}"; shift 2 ;;
        --repo-url)        GOCP_REPO_URL="${2:?falta el valor}"; shift 2 ;;
        --repo-ref)        GOCP_REPO_REF="${2:?falta el valor}"; shift 2 ;;
        --unattended)      UNATTENDED=1; shift ;;
        --skip-docker-install) SKIP_DOCKER_INSTALL=1; shift ;;
        --skip-images)     SKIP_IMAGES=1; shift ;;
        --no-tls)          NO_TLS=1; shift ;;
        --dry-run)         DRY_RUN=1; shift ;;
        -h|--help)         uso; exit 0 ;;
        *) uso >&2; morir "Opción desconocida: $1" ;;
    esac
done
 
ejecutar() {
    if [[ $DRY_RUN -eq 1 ]]; then
        printf '%s    [simulación] %s%s\n' "$C_DIM" "$*" "$C_RESET"
        return 0
    fi
    log "EXEC  $*"
    "$@"
}
 
preguntar() {
    # preguntar <variable> <texto> [valor por defecto] [validador]
    local -n destino="$1"
    local texto="$2" defecto="${3:-}" validador="${4:-}"
 
    if [[ -n "$destino" ]]; then
        return 0   # ya viene por flag o entorno
    fi
    if [[ $UNATTENDED -eq 1 ]]; then
        if [[ -n "$defecto" ]]; then
            destino="$defecto"
            return 0
        fi
        morir "Falta un dato obligatorio en modo desatendido: $texto"
    fi
 
    local valor
    while true; do
        if [[ -n "$defecto" ]]; then
            read -r -p "  $texto [$defecto]: " valor </dev/tty || true
            valor="${valor:-$defecto}"
        else
            read -r -p "  $texto: " valor </dev/tty || true
        fi
        if [[ -z "$valor" ]]; then
            aviso "Este dato es obligatorio."
            continue
        fi
        if [[ -n "$validador" ]] && ! "$validador" "$valor"; then
            continue
        fi
        destino="$valor"
        return 0
    done
}
 
confirmar() {
    # confirmar <texto> ; devuelve 0 si sí
    [[ $UNATTENDED -eq 1 ]] && return 0
    local respuesta
    read -r -p "  $1 [S/n]: " respuesta </dev/tty || true
    [[ -z "$respuesta" || "$respuesta" =~ ^[SsYy] ]]
}
 
# ──────────────────────────────────────────────────────────────────────────
# Validadores
# ──────────────────────────────────────────────────────────────────────────
 
validar_dominio() {
    if [[ "$1" =~ ^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$ ]]; then
        return 0
    fi
    if [[ $NO_TLS -eq 1 && "$1" =~ ^(localhost|[a-z0-9.-]+)$ ]]; then
        return 0
    fi
    aviso "'$1' no parece un dominio válido (ejemplo: panel.midominio.cl)."
    return 1
}
 
validar_correo() {
    [[ "$1" =~ ^[^@[:space:]]+@[^@[:space:]]+\.[a-zA-Z]{2,}$ ]] && return 0
    aviso "'$1' no parece un correo válido."
    return 1
}
 
validar_password() {
    if [[ ${#1} -lt 10 ]]; then
        aviso "La contraseña debe tener al menos 10 caracteres."
        return 1
    fi
    return 0
}
 
# ──────────────────────────────────────────────────────────────────────────
# 1. Comprobaciones previas
# ──────────────────────────────────────────────────────────────────────────
 
comprobar_root() {
    if [[ $EUID -ne 0 ]]; then
        morir "Este instalador necesita privilegios de root." \
              "Vuelve a ejecutarlo con: sudo $0 $*"
    fi
}
 
detectar_so() {
    if [[ ! -r /etc/os-release ]]; then
        morir "No se pudo identificar la distribución (falta /etc/os-release)."
    fi
    # shellcheck disable=SC1091
    . /etc/os-release
    SO_ID="${ID:-desconocido}"
    SO_NOMBRE="${PRETTY_NAME:-$SO_ID}"
    SO_FAMILIA="${ID_LIKE:-$SO_ID}"
 
    case " $SO_ID $SO_FAMILIA " in
        *debian*|*ubuntu*|*rhel*|*fedora*|*centos*|*almalinux*|*rocky*) : ;;
        *) aviso "Distribución no probada ($SO_NOMBRE). El instalador seguirá, pero puede fallar." ;;
    esac
    ok "Sistema: $SO_NOMBRE ($(uname -m))"
}
 
comprobar_recursos() {
    local ram_mb disco_gb
    ram_mb=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)
    disco_gb=$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9' || echo 0)
 
    if [[ "$ram_mb" -lt 1800 ]]; then
        aviso "Solo hay ${ram_mb} MB de RAM. Se recomiendan 2 GB como mínimo y 8 GB para producción."
        confirmar "¿Continuar de todos modos?" || exit 1
    else
        ok "Memoria: ${ram_mb} MB"
    fi
 
    if [[ "$disco_gb" -lt 20 ]]; then
        aviso "Solo quedan ${disco_gb} GB libres en /. Se recomiendan 20 GB."
        confirmar "¿Continuar de todos modos?" || exit 1
    else
        ok "Disco libre: ${disco_gb} GB"
    fi
}
 
comprobar_puertos() {
    local ocupados=()
    for puerto in 80 443; do
        if command -v ss >/dev/null 2>&1 && ss -Hltn "sport = :$puerto" 2>/dev/null | grep -q .; then
            ocupados+=("$puerto")
        fi
    done
    if [[ ${#ocupados[@]} -gt 0 ]]; then
        local quien
        quien=$(ss -Hltnp "sport = :${ocupados[0]}" 2>/dev/null | head -1 || true)
        aviso "Los puertos ${ocupados[*]} están ocupados. GoControlPanel los necesita."
        [[ -n "$quien" ]] && info "$C_DIM$quien$C_RESET"
        info "Detén el servicio que los usa (Apache, Nginx…) antes de continuar."
        confirmar "¿Continuar igualmente?" || exit 1
    else
        ok "Puertos 80 y 443 disponibles"
    fi
}
 
comprobar_herramientas() {
    local faltan=()
    for cmd in curl openssl; do
        command -v "$cmd" >/dev/null 2>&1 || faltan+=("$cmd")
    done
    if [[ ${#faltan[@]} -gt 0 ]]; then
        info "Instalando herramientas necesarias: ${faltan[*]}"
        instalar_paquetes "${faltan[@]}"
    fi
    ok "Herramientas básicas disponibles"
}
 
instalar_paquetes() {
    case " $SO_ID $SO_FAMILIA " in
        *debian*|*ubuntu*)
            ejecutar env DEBIAN_FRONTEND=noninteractive apt-get update -qq
            ejecutar env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
            ;;
        *rhel*|*fedora*|*centos*|*almalinux*|*rocky*)
            if command -v dnf >/dev/null 2>&1; then
                ejecutar dnf install -y -q "$@"
            else
                ejecutar yum install -y -q "$@"
            fi
            ;;
        *)
            morir "No sé instalar paquetes en esta distribución. Instala a mano: $*"
            ;;
    esac
}
 
# ──────────────────────────────────────────────────────────────────────────
# 2. Docker
# ──────────────────────────────────────────────────────────────────────────
 
comprobar_docker() {
    if [[ $DRY_RUN -eq 1 ]] && ! docker info >/dev/null 2>&1; then
        aviso "Docker no está disponible, pero en modo simulación se continúa igualmente."
        return 0
    fi
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        if ! docker info >/dev/null 2>&1; then
            info "Docker está instalado pero el demonio no responde; intentando arrancarlo…"
            ejecutar systemctl enable --now docker || true
            sleep 3
            docker info >/dev/null 2>&1 || morir "El demonio de Docker no arranca." \
                "Revisa: systemctl status docker"
        fi
        ok "Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo '') con Compose v2"
        return 0
    fi
 
    if [[ $SKIP_DOCKER_INSTALL -eq 1 ]]; then
        morir "Falta Docker con Compose v2 y se pidió no instalarlo." \
              "Instálalo con: curl -fsSL https://get.docker.com | sh"
    fi
 
    aviso "No se encontró Docker con Compose v2."
    if ! confirmar "¿Instalar Docker ahora usando el script oficial (get.docker.com)?"; then
        morir "Docker es imprescindible para GoControlPanel."
    fi
 
    info "Descargando e instalando Docker…"
    if [[ $DRY_RUN -eq 1 ]]; then
        printf '%s    [simulación] curl -fsSL https://get.docker.com | sh%s\n' "$C_DIM" "$C_RESET"
    else
        curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
        sh /tmp/get-docker.sh >>"$LOG_FILE" 2>&1 || morir "La instalación de Docker falló. Revisa $LOG_FILE"
        rm -f /tmp/get-docker.sh
        systemctl enable --now docker >>"$LOG_FILE" 2>&1 || true
    fi
 
    if [[ $DRY_RUN -eq 0 ]]; then
        docker compose version >/dev/null 2>&1 || \
            morir "Docker quedó instalado pero falta el plugin Compose v2." \
                  "Instálalo con el paquete docker-compose-plugin de tu distribución."
    fi
    ok "Docker instalado"
}
 
# ──────────────────────────────────────────────────────────────────────────
# 3. Código fuente
# ──────────────────────────────────────────────────────────────────────────
 
obtener_codigo() {
    if [[ -f "$SCRIPT_DIR/docker-compose.yml" && -d "$SCRIPT_DIR/cmd/gocpd" ]]; then
        # Ejecutándose dentro del repositorio.
        if [[ "$SCRIPT_DIR" == "$GOCP_INSTALL_DIR" ]]; then
            ok "Usando el proyecto ya presente en $GOCP_INSTALL_DIR"
        else
            info "Copiando el proyecto a $GOCP_INSTALL_DIR…"
            ejecutar mkdir -p "$GOCP_INSTALL_DIR"
            if command -v rsync >/dev/null 2>&1; then
                ejecutar rsync -a --exclude '.git' --exclude 'web/node_modules' \
                    "$SCRIPT_DIR"/ "$GOCP_INSTALL_DIR"/
            else
                ejecutar cp -a "$SCRIPT_DIR"/. "$GOCP_INSTALL_DIR"/
                ejecutar rm -rf "$GOCP_INSTALL_DIR/.git" "$GOCP_INSTALL_DIR/web/node_modules"
            fi
            ok "Proyecto copiado a $GOCP_INSTALL_DIR"
        fi
        return 0
    fi
 
    # Servidor limpio: clonar.
    command -v git >/dev/null 2>&1 || instalar_paquetes git
 
    if [[ -d "$GOCP_INSTALL_DIR/.git" ]]; then
        info "Actualizando el repositorio existente…"
        ejecutar git -C "$GOCP_INSTALL_DIR" fetch --depth 1 origin "$GOCP_REPO_REF"
        ejecutar git -C "$GOCP_INSTALL_DIR" reset --hard FETCH_HEAD
    else
        info "Clonando $GOCP_REPO_URL (rama $GOCP_REPO_REF)…"
        ejecutar git clone --depth 1 --branch "$GOCP_REPO_REF" \
            "$GOCP_REPO_URL" "$GOCP_INSTALL_DIR"
    fi
    ok "Código en $GOCP_INSTALL_DIR"
}
 
# ──────────────────────────────────────────────────────────────────────────
# 4. Configuración
# ──────────────────────────────────────────────────────────────────────────
 
recoger_datos() {
    if [[ $NO_TLS -eq 1 && -z "$GOCP_DOMAIN" ]]; then
        GOCP_DOMAIN="localhost"
    fi
 
    preguntar GOCP_DOMAIN "Dominio del panel (apuntado ya a este servidor)" "" validar_dominio
 
    if [[ $NO_TLS -eq 0 ]]; then
        preguntar GOCP_EMAIL "Correo para los certificados Let's Encrypt" "" validar_correo
    fi
 
    preguntar GOCP_ADMIN_USER "Usuario administrador" "admin"
 
    if [[ -z "$GOCP_ADMIN_PASSWORD" ]]; then
        if [[ $UNATTENDED -eq 1 ]]; then
            GOCP_ADMIN_PASSWORD="$(generar_secreto 18)"
            GENERATED_PASSWORD="$GOCP_ADMIN_PASSWORD"
        else
            local p1 p2
            while true; do
                read -r -s -p "  Contraseña del administrador (vacío = generar): " p1 </dev/tty || true
                printf '\n'
                if [[ -z "$p1" ]]; then
                    GOCP_ADMIN_PASSWORD="$(generar_secreto 18)"
                    GENERATED_PASSWORD="$GOCP_ADMIN_PASSWORD"
                    ok "Contraseña generada automáticamente (se mostrará al final)"
                    break
                fi
                validar_password "$p1" || continue
                read -r -s -p "  Repite la contraseña: " p2 </dev/tty || true
                printf '\n'
                if [[ "$p1" != "$p2" ]]; then
                    aviso "Las contraseñas no coinciden."
                    continue
                fi
                GOCP_ADMIN_PASSWORD="$p1"
                break
            done
        fi
    else
        validar_password "$GOCP_ADMIN_PASSWORD" || morir "La contraseña del administrador es demasiado corta."
    fi
}
 
generar_secreto() {
    local n="${1:-32}"
    openssl rand -base64 48 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c "$n"
}
 
escribir_env() {
    local env_file="$GOCP_INSTALL_DIR/.env"
 
    if [[ -f "$env_file" ]]; then
        local respaldo
        respaldo="$env_file.$(date +%Y%m%d%H%M%S).bak"
        info "Ya existía un .env; se guarda copia en $(basename "$respaldo")"
        ejecutar cp "$env_file" "$respaldo"
 
        # Conservamos las contraseñas de las bases de datos: cambiarlas dejaría
        # inaccesibles los volúmenes de datos ya inicializados.
        POSTGRES_PASSWORD="$(grep -E '^POSTGRES_PASSWORD=' "$env_file" | cut -d= -f2- || true)"
        MYSQL_ROOT_PASSWORD="$(grep -E '^MYSQL_ROOT_PASSWORD=' "$env_file" | cut -d= -f2- || true)"
        JWT_SECRET="$(grep -E '^GOCP_JWT_SECRET=' "$env_file" | cut -d= -f2- || true)"
    fi
 
    POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(generar_secreto 32)}"
    MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-$(generar_secreto 32)}"
    JWT_SECRET="${JWT_SECRET:-$(openssl rand -hex 32)}"
 
    local esquema="https"
    [[ $NO_TLS -eq 1 ]] && esquema="http"
 
    if [[ $DRY_RUN -eq 1 ]]; then
        printf '%s    [simulación] escribir %s%s\n' "$C_DIM" "$env_file" "$C_RESET"
        return 0
    fi
 
    umask 077
    cat >"$env_file" <<EOF
# Generado por install.sh el $(date -Iseconds)
# Contiene secretos: no lo subas a ningún repositorio.
 
GOCP_ENV=production
GOCP_PUBLIC_URL=${esquema}://${GOCP_DOMAIN}
GOCP_LISTEN_ADDR=:8080
 
GOCP_JWT_SECRET=${JWT_SECRET}
 
GOCP_ADMIN_USER=${GOCP_ADMIN_USER}
GOCP_ADMIN_EMAIL=${GOCP_EMAIL:-admin@${GOCP_DOMAIN}}
GOCP_ADMIN_PASSWORD=${GOCP_ADMIN_PASSWORD}
 
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD}
 
GOCP_CADDY_EMAIL=${GOCP_EMAIL}
 
GOCP_DOCKER_NETWORK=gocontrolpanel_sites
GOCP_SITE_IMAGE_PREFIX=gocp/frankenphp
GOCP_SITES_ROOT=${GOCP_DATA_DIR}
GOCP_PANEL_UPSTREAM=gocp-panel:8080
 
GOCP_ACCESS_TTL=15m
GOCP_REFRESH_TTL=720h
GOCP_METRICS_INTERVAL=60s
GOCP_BCRYPT_COST=12
EOF
    chmod 600 "$env_file"
    ok "Configuración escrita en $env_file"
}
 
preparar_directorios() {
    ejecutar mkdir -p "$GOCP_DATA_DIR"
    ejecutar chmod 750 "$GOCP_DATA_DIR"
    ok "Directorio de cuentas: $GOCP_DATA_DIR"
 
    # docker-compose.yml monta esta ruta en el mismo punto dentro y fuera del
    # contenedor. Si el usuario la cambió, hay que reflejarlo en el compose.
    if [[ "$GOCP_DATA_DIR" != "/srv/gocp/accounts" && $DRY_RUN -eq 0 ]]; then
        info "Ajustando docker-compose.yml para usar $GOCP_DATA_DIR"
        sed -i "s#/srv/gocp/accounts:/srv/gocp/accounts#${GOCP_DATA_DIR}:${GOCP_DATA_DIR}#" \
            "$GOCP_INSTALL_DIR/docker-compose.yml"
    fi
}
 
# ──────────────────────────────────────────────────────────────────────────
# 5. Construcción y arranque
# ──────────────────────────────────────────────────────────────────────────
 
construir_imagenes() {
    if [[ $SKIP_IMAGES -eq 1 ]]; then
        aviso "Se omite la construcción de imágenes de PHP (--skip-images)."
        aviso "Créalas después con: cd $GOCP_INSTALL_DIR && make images"
        return 0
    fi
    info "Construyendo las imágenes FrankenPHP ($GOCP_PHP_VERSIONS). Esto tarda varios minutos…"
    for v in $GOCP_PHP_VERSIONS; do
        info "  → gocp/frankenphp:php$v"
        if [[ $DRY_RUN -eq 1 ]]; then
            printf '%s    [simulación] docker build php%s%s\n' "$C_DIM" "$v" "$C_RESET"
            continue
        fi
        docker build --build-arg "PHP_VERSION=$v" \
            -t "gocp/frankenphp:php$v" \
            "$GOCP_INSTALL_DIR/deploy/site-image" >>"$LOG_FILE" 2>&1 \
            || morir "Falló la construcción de la imagen de PHP $v. Revisa $LOG_FILE"
    done
    ok "Imágenes de PHP listas"
}
 
levantar_plataforma() {
    if [[ $DRY_RUN -eq 1 ]]; then
        printf '%s    [simulación] docker compose up -d --build%s\n' "$C_DIM" "$C_RESET"
        return 0
    fi
 
    # Validar antes de construir: si falta una variable o el YAML está mal, es
    # mejor saberlo ahora que tras varios minutos de compilación.
    local salida_config
    if ! salida_config=$( ( cd "$GOCP_INSTALL_DIR" && docker compose config -q ) 2>&1 ); then
        morir "La configuración de docker compose no es válida: $salida_config" \
              "Revisa $GOCP_INSTALL_DIR/docker-compose.yml y $GOCP_INSTALL_DIR/.env"
    fi
    ok "Configuración de Docker Compose validada"
 
    info "Construyendo el panel y levantando los servicios…"
    ( cd "$GOCP_INSTALL_DIR" && docker compose up -d --build ) >>"$LOG_FILE" 2>&1 \
        || morir "docker compose falló. Revisa $LOG_FILE" \
                 "También puedes ver los logs con: cd $GOCP_INSTALL_DIR && docker compose logs"
    ok "Servicios en marcha"
}
 
esperar_salud() {
    [[ $DRY_RUN -eq 1 ]] && return 0
 
    info "Esperando a que el panel responda…"
    # La imagen del panel es distroless: no tiene shell ni wget. El chequeo se
    # hace desde el contenedor del borde, que sí trae busybox y está en la
    # misma red que el panel.
    local intentos=60 respuesta=""
    for ((i = 1; i <= intentos; i++)); do
        respuesta=$( ( cd "$GOCP_INSTALL_DIR" && docker compose exec -T edge \
            wget -qO- --timeout=3 http://gocp-panel:8080/api/v1/health ) 2>/dev/null || true)
        if [[ "$respuesta" == *'"status"'* ]]; then
            if [[ "$respuesta" == *'"ok"'* ]]; then
                ok "El panel responde correctamente"
                return 0
            fi
            aviso "El panel arrancó en estado degradado: $respuesta"
            return 0
        fi
        sleep 3
    done
 
    aviso "El panel no respondió tras $((intentos * 3)) segundos."
    info "Comprueba el estado con: cd $GOCP_INSTALL_DIR && docker compose ps"
    info "Y los registros con:     cd $GOCP_INSTALL_DIR && docker compose logs panel"
    return 0
}
 
instalar_cli() {
    local destino=/usr/local/bin/gocp
    if [[ ! -f "$GOCP_INSTALL_DIR/deploy/gocp" ]]; then
        return 0
    fi
    ejecutar install -m 0755 "$GOCP_INSTALL_DIR/deploy/gocp" "$destino"
    if [[ $DRY_RUN -eq 0 ]]; then
        sed -i "s#^GOCP_INSTALL_DIR=.*#GOCP_INSTALL_DIR=\"\${GOCP_INSTALL_DIR:-$GOCP_INSTALL_DIR}\"#" "$destino"
    fi
    ok "Comando de gestión instalado: gocp"
}
 
comprobar_dns() {
    [[ $NO_TLS -eq 1 ]] && return 0
    command -v getent >/dev/null 2>&1 || return 0
 
    local resuelto ip_publica
    resuelto=$(getent hosts "$GOCP_DOMAIN" 2>/dev/null | awk '{print $1}' | head -1 || true)
    ip_publica=$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)
 
    if [[ -z "$resuelto" ]]; then
        aviso "$GOCP_DOMAIN todavía no resuelve. Los certificados no se emitirán hasta que el DNS apunte aquí."
    elif [[ -n "$ip_publica" && "$resuelto" != "$ip_publica" ]]; then
        aviso "$GOCP_DOMAIN resuelve a $resuelto, pero la IP pública de este servidor es $ip_publica."
        info "Si usas Cloudflare en modo proxy esto es normal; en caso contrario, corrige el registro A."
    else
        ok "DNS correcto: $GOCP_DOMAIN → $resuelto"
    fi
}
 
resumen_final() {
    local esquema="https"
    [[ $NO_TLS -eq 1 ]] && esquema="http"
 
    printf '\n%s%s╭─────────────────────────────────────────────────────────────╮%s\n' \
        "$C_BOLD" "$C_GREEN" "$C_RESET"
    printf '%s%s│  Instalación completada                                     │%s\n' \
        "$C_BOLD" "$C_GREEN" "$C_RESET"
    printf '%s%s╰─────────────────────────────────────────────────────────────╯%s\n\n' \
        "$C_BOLD" "$C_GREEN" "$C_RESET"
 
    printf '  %sPanel:%s      %s://%s\n' "$C_BOLD" "$C_RESET" "$esquema" "$GOCP_DOMAIN"
    printf '  %sUsuario:%s    %s\n' "$C_BOLD" "$C_RESET" "$GOCP_ADMIN_USER"
    if [[ -n "$GENERATED_PASSWORD" ]]; then
        printf '  %sContraseña:%s %s%s%s  %s(generada, guárdala ahora)%s\n' \
            "$C_BOLD" "$C_RESET" "$C_BOLD$C_YELLOW" "$GENERATED_PASSWORD" "$C_RESET" \
            "$C_DIM" "$C_RESET"
    else
        printf '  %sContraseña:%s la que indicaste durante la instalación\n' "$C_BOLD" "$C_RESET"
    fi
 
    printf '\n  %sInstalado en:%s   %s\n' "$C_BOLD" "$C_RESET" "$GOCP_INSTALL_DIR"
    printf '  %sDatos de clientes:%s %s\n' "$C_BOLD" "$C_RESET" "$GOCP_DATA_DIR"
    printf '  %sRegistro:%s       %s\n' "$C_BOLD" "$C_RESET" "$LOG_FILE"
 
    printf '\n  %sGestión:%s\n' "$C_BOLD" "$C_RESET"
    printf '    gocp status          estado de los servicios\n'
    printf '    gocp logs            registros del panel\n'
    printf '    gocp restart         reiniciar la plataforma\n'
    printf '    gocp backup          copia de seguridad completa\n'
    printf '    gocp update          actualizar a la última versión\n'
    printf '    gocp password        cambiar la contraseña del administrador\n'
 
    if [[ $NO_TLS -eq 0 ]]; then
        printf '\n  %sSiguiente paso:%s abre https://%s en el navegador.\n' \
            "$C_BOLD" "$C_RESET" "$GOCP_DOMAIN"
        printf '  El certificado se emite en el primer acceso; puede tardar unos segundos.\n'
    else
        printf '\n  %sModo sin TLS:%s el panel escucha en http://%s\n' \
            "$C_BOLD" "$C_RESET" "$GOCP_DOMAIN"
    fi
 
    printf '\n  %sAntes de dar de alta clientes reales, revisa docs/07-seguridad.md:%s\n' \
        "$C_DIM" "$C_RESET"
    printf '  %sel panel tiene acceso al socket de Docker, que equivale a root en el host.%s\n\n' \
        "$C_DIM" "$C_RESET"
}
 
# ──────────────────────────────────────────────────────────────────────────
# Flujo principal
# ──────────────────────────────────────────────────────────────────────────
 
main() {
    banner
    comprobar_root "$@"
 
    touch "$LOG_FILE" 2>/dev/null || LOG_FILE=/tmp/gocontrolpanel-install.log
    log "===== Instalación iniciada $(date -Iseconds) ====="
 
    [[ $DRY_RUN -eq 1 ]] && aviso "Modo simulación: no se modificará nada en el sistema."
 
    paso "1/8  Comprobando el sistema"
    detectar_so
    comprobar_recursos
    comprobar_puertos
    comprobar_herramientas
 
    paso "2/8  Docker"
    comprobar_docker
 
    paso "3/8  Datos de la instalación"
    recoger_datos
    ok "Panel: $GOCP_DOMAIN · administrador: $GOCP_ADMIN_USER"
 
    paso "4/8  Obteniendo el código"
    obtener_codigo
 
    paso "5/8  Configuración"
    escribir_env
    preparar_directorios
 
    paso "6/8  Imágenes de PHP"
    construir_imagenes
 
    paso "7/8  Arrancando la plataforma"
    levantar_plataforma
    esperar_salud
    instalar_cli
 
    paso "8/8  Comprobaciones finales"
    comprobar_dns
 
    log "===== Instalación finalizada $(date -Iseconds) ====="
    resumen_final
}
 
# Solo se ejecuta al invocar el script; al hacer `source install.sh` quedan
# disponibles las funciones, que es como las prueba tools/test-install.sh.
if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
    main "$@"
fi
 