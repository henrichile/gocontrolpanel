# GoControlPanel

Panel de control de hosting estilo WHM/cPanel, escrito en **Go**, que sirve los
sitios PHP con **FrankenPHP** y usa **Caddy** como servidor de borde (TLS
automático y reverse proxy).

Cada sitio corre en su **propio contenedor FrankenPHP**, con límites de CPU y
memoria tomados del plan de la cuenta. El panel nunca sirve PHP directamente:
publica el enrutado en Caddy a través de su Admin API y deja que el borde haga
el TLS.

```
Internet
   │  :80 / :443
   ▼
┌──────────────────────┐      Admin API (:2019)      ┌────────────────────┐
│  Caddy (borde)       │◄────────────────────────────│  Panel (Go)        │
│  TLS + reverse proxy │                             │  API REST + SPA    │
└─────────┬────────────┘                             └────┬───────┬───────┘
          │ http interno                        Docker SDK│       │ SQL
   ┌──────┴───────┬──────────────┐                        │       │
   ▼              ▼              ▼                        ▼       ▼
┌────────┐   ┌────────┐   ┌────────┐               ┌──────────┐ ┌──────────┐
│Frankn  │   │Frankn  │   │Frankn  │  …un           │  Docker  │ │PostgreSQL│
│PHP #1  │   │PHP #2  │   │PHP #n  │  contenedor    │  engine  │ │  (panel) │
│:8080   │   │:8080   │   │:8080   │  por sitio     └──────────┘ └──────────┘
└────────┘   └────────┘   └────────┘
```

## Qué incluye

| Área | Estado |
|---|---|
| Autenticación JWT con refresh rotativo y rate limiting | ✅ |
| Roles admin / revendedor / usuario, con alcance por propietario | ✅ |
| Planes de hosting (disco, sitios, BD, CPU, RAM, versiones de PHP) | ✅ |
| Cuentas de hosting: crear, suspender, reactivar, terminar | ✅ |
| Sitios: crear, arrancar, detener, reiniciar, redesplegar, borrar | ✅ |
| Dominios y subdominios con TLS automático (ACME + on-demand autorizado) | ✅ |
| Bases de datos MySQL/MariaDB por cuenta, con usuario dedicado | ✅ |
| Tareas cron por sitio, ejecutadas dentro del contenedor | ✅ |
| Registros del contenedor, en bloque y en vivo (SSE) | ✅ |
| Métricas de CPU/memoria por sitio e histórico de 30 días | ✅ |
| Bitácora de auditoría de todas las acciones sensibles | ✅ |
| Reconciliación automática del estado real de los contenedores | ✅ |
| SPA React + TypeScript embebida en el binario | ✅ |
| Acceso SFTP por cuenta (usuarios virtuales vía sftpgo, sin usuarios Unix reales) | ✅ |
| Correo, copias de seguridad automatizadas, explorador de archivos en el panel | ⏳ pendiente |

## Puesta en marcha

Requisitos: Docker con Compose v2, y un servidor Linux con los puertos 80/443
libres.

```bash
git clone <tu-repo> gocontrolpanel && cd gocontrolpanel

cp .env.example .env
openssl rand -hex 32                 # → GOCP_JWT_SECRET
openssl rand -base64 24              # → POSTGRES_PASSWORD / MYSQL_ROOT_PASSWORD
$EDITOR .env

sudo mkdir -p /srv/gocp/accounts     # raíz de los archivos de los clientes

make images                          # imágenes FrankenPHP (PHP 8.3 y 8.4)
make up                              # panel + Caddy + PostgreSQL + MariaDB
```

Apunta el DNS de `GOCP_PUBLIC_URL` al servidor y entra con el usuario y la
contraseña de `GOCP_ADMIN_USER` / `GOCP_ADMIN_PASSWORD`. Si dejaste la
contraseña vacía, créala después con:

```bash
docker compose exec panel gocpd createadmin
```

### Desarrollo local

```bash
# base de datos y borde
docker compose up -d postgres mysql edge

# API en :8080 con recarga manual
make run

# SPA en :5173, con proxy /api hacia :8080
make dev-web
```

## Estructura del proyecto

```
cmd/gocpd/            binario del panel (servidor + subcomandos CLI)
internal/
  api/                router chi, middleware, handlers REST, servidor de la SPA
  auth/               bcrypt, JWT, refresh tokens, identidad en contexto
  caddyapi/           cliente de la Admin API y generador de config JSON
  config/             configuración por entorno
  database/           pool de PostgreSQL y migraciones embebidas
  dockerx/            orquestación de los contenedores FrankenPHP
  httpx/              helpers de JSON y errores
  models/             tipos de dominio
  provision/          lógica de negocio (cuentas, sitios, dominios, MySQL, SFTP)
  store/              acceso a datos
  sysinfo/            métricas del host desde /proc
  worker/             métricas, cron, reconciliación, limpieza
web/                  SPA React + TypeScript (Vite), embebida en el binario
deploy/
  edge/Caddyfile      configuración de arranque del borde
  site-image/         Dockerfile + php.ini de la imagen de los sitios
```

## Cómo se enruta un sitio

1. El panel crea el contenedor `gocp-site-<cuenta>-<sitio>` en la red
   `gocontrolpanel_sites`, con `/app` montado desde
   `/srv/gocp/accounts/<cuenta>/sites/<sitio>`.
2. FrankenPHP escucha en `:8080` dentro del contenedor, en HTTP plano.
3. `SyncCaddy` reconstruye la configuración completa del borde desde la base de
   datos y la publica con `POST /load` en la Admin API. No hay reinicios ni
   archivos generados a mano.
4. Caddy resuelve el upstream por el alias de red del contenedor
   (`gocp-site-…:8080`) y emite el certificado.

Un sitio detenido no desaparece del enrutado: se sirve una página 503 propia en
lugar de un error de conexión.

## Seguridad

Puntos que conviene tener presentes antes de exponer esto a clientes reales:

- **El panel monta el socket de Docker.** Eso equivale a acceso root en el host.
  Publica el panel solo por HTTPS, restringe quién tiene rol `admin` y considera
  un proxy de socket con permisos acotados si vas a delegar la administración.
- **Certificados on-demand autorizados.** Caddy consulta
  `/api/v1/tls/authorize` antes de emitir; solo los dominios registrados en el
  panel obtienen certificado, así nadie puede apuntar un DNS ajeno y agotar la
  cuota de ACME.
- **Contenedores endurecidos**: `no-new-privileges`, todas las capabilities
  eliminadas salvo las mínimas, límite de PIDs, memoria sin swap extra y logs
  rotados.
- **Contraseñas** con bcrypt (coste 12 por defecto); los refresh tokens se
  guardan solo como hash SHA-256 y se rotan en cada renovación.
- **Aislamiento entre clientes**: un contenedor por sitio y un usuario MySQL por
  base de datos, con permisos limitados a esa base.

Pendiente para producción seria: cuotas de disco reales (XFS project quotas o
`quota` por usuario), WAF/rate limiting en el borde, y 2FA en el panel (el campo
`totp_secret` ya existe en el esquema).

## API

Todos los endpoints cuelgan de `/api/v1` y usan `Authorization: Bearer <token>`.

| Método | Ruta | Descripción |
|---|---|---|
| POST | `/auth/login` | Inicia sesión, devuelve access + refresh |
| POST | `/auth/refresh` | Rota el refresh token |
| GET | `/overview` | Contadores del dashboard |
| GET/POST | `/accounts` | Lista y crea cuentas de hosting |
| POST | `/accounts/{id}/suspend` | Suspende la cuenta y detiene sus sitios |
| GET/POST | `/sites` | Lista y crea sitios |
| POST | `/sites/{id}/{start\|stop\|restart\|redeploy}` | Control del contenedor |
| GET | `/sites/{id}/logs?follow=true` | Registros en vivo (SSE) |
| POST | `/sites/{id}/domains` | Añade un dominio y recarga Caddy |
| GET/POST | `/accounts/{id}/databases` | Bases de datos MySQL de la cuenta |
| GET/POST | `/accounts/{id}/ftp` | Accesos SFTP de la cuenta (usuario, host, puerto) |
| GET | `/system/info` | Métricas del host (solo admin) |
| POST | `/system/caddy/sync` | Fuerza la republicación del enrutado |

## Licencia

Pendiente de definir por Etasoft.
