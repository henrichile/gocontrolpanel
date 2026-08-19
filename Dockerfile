# --- Etapa 1: build de la SPA ---------------------------------------------
FROM node:22-alpine AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- Etapa 2: build del binario Go ----------------------------------------
FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# El build de la SPA reemplaza el placeholder embebido.
COPY --from=web /src/web/dist ./web/dist

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/gocpd ./cmd/gocpd

# --- Etapa 3: imagen final -------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/gocpd /usr/local/bin/gocpd

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gocpd"]
