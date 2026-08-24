// Package dockerx encapsula la orquestación de los contenedores FrankenPHP:
// uno por sitio, con límites de CPU/memoria y su raíz montada desde el host.
package dockerx

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	// Puerto interno en el que escucha FrankenPHP dentro del contenedor.
	SitePort = "8080"

	LabelManaged = "gocp.managed"
	LabelSiteID  = "gocp.site_id"
	LabelAccount = "gocp.account"

	// SiteUID es el UID:GID no-root con el que corren todos los contenedores
	// de sitio — el mismo que usa el propio panel (imagen distroless
	// "nonroot", ver el chown en docker-compose.yml). Al ser un UID numérico
	// fijo en vez de un usuario con nombre (p.ej. "www-data"), no depende de
	// que la imagen del sitio traiga ese usuario creado, y coincide con el
	// dueño real de los directorios que el panel crea en el host.
	SiteUID = "65532:65532"
)

type Manager struct {
	cli     *client.Client
	network string
}

func New(dockerHost, networkName string) (*Manager, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if dockerHost != "" {
		opts = append(opts, client.WithHost(dockerHost))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el cliente de Docker: %w", err)
	}
	return &Manager{cli: cli, network: networkName}, nil
}

func (m *Manager) Close() error { return m.cli.Close() }

func (m *Manager) Ping(ctx context.Context) error {
	_, err := m.cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("Docker no responde: %w", err)
	}
	return nil
}

// EnsureNetwork crea la red bridge de los sitios si aún no existe.
func (m *Manager) EnsureNetwork(ctx context.Context) error {
	nets, err := m.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", m.network)),
	})
	if err != nil {
		return err
	}
	for _, n := range nets {
		if n.Name == m.network {
			return nil
		}
	}
	_, err = m.cli.NetworkCreate(ctx, m.network, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{LabelManaged: "true"},
	})
	return err
}

// SiteSpec describe el contenedor que hay que levantar para un sitio.
type SiteSpec struct {
	Name          string // nombre del contenedor, único
	Image         string
	SiteID        string
	AccountUser   string
	HostPath      string            // directorio en el host montado en /app
	DocumentRoot  string            // subcarpeta servida, relativa a /app
	Env           map[string]string
	WorkerScript  string
	WorkerCount   int
	CPULimit      float64 // núcleos
	MemoryLimitMB int64
	ReadOnlyRoot  bool
}

// CreateOrReplace crea el contenedor del sitio, eliminando cualquier versión
// anterior con el mismo nombre. Devuelve el ID del contenedor.
func (m *Manager) CreateOrReplace(ctx context.Context, spec SiteSpec) (string, error) {
	if err := m.RemoveIfExists(ctx, spec.Name); err != nil {
		return "", err
	}
	if err := m.EnsureImage(ctx, spec.Image); err != nil {
		return "", err
	}

	docRoot := strings.Trim(spec.DocumentRoot, "/")
	if docRoot == "" {
		docRoot = "public"
	}

	env := []string{
		"SERVER_NAME=:" + SitePort,
		"SERVER_ROOT=/app/" + docRoot,
		"FRANKENPHP_NO_COMPRESS=0",
		"CADDY_GLOBAL_OPTIONS=auto_https off",
		// El contenedor corre como SiteUID, un UID numérico sin usuario ni
		// home asociado en /etc/passwd: sin esto, git/composer intentarían
		// escribir su caché en un $HOME inexistente.
		"HOME=/tmp",
	}
	if spec.WorkerScript != "" {
		env = append(env, "FRANKENPHP_CONFIG=worker /app/"+strings.TrimLeft(spec.WorkerScript, "/"))
		if spec.WorkerCount > 0 {
			env = append(env, fmt.Sprintf("FRANKENPHP_WORKERS=%d", spec.WorkerCount))
		}
	}
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	cfg := &container.Config{
		Image: spec.Image,
		// Sin privilegios: el proceso principal de FrankenPHP corre como
		// SiteUID, no como root de la imagen upstream.
		User:   SiteUID,
		Env:    env,
		Labels: map[string]string{
			LabelManaged: "true",
			LabelSiteID:  spec.SiteID,
			LabelAccount: spec.AccountUser,
		},
		ExposedPorts: nat.PortSet{nat.Port(SitePort + "/tcp"): struct{}{}},
		WorkingDir:   "/app",
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: spec.HostPath,
			Target: "/app",
		}},
		Resources: container.Resources{
			NanoCPUs: int64(spec.CPULimit * 1e9),
			Memory:   spec.MemoryLimitMB * 1024 * 1024,
			// Sin swap adicional: el límite de memoria es duro.
			MemorySwap: spec.MemoryLimitMB * 1024 * 1024,
			PidsLimit:  ptr(int64(512)),
		},
		// Endurecimiento: sin privilegios extra ni escalada.
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"ALL"},
		CapAdd:      []string{"CHOWN", "SETUID", "SETGID", "NET_BIND_SERVICE"},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}
	if spec.ReadOnlyRoot {
		hostCfg.ReadonlyRootfs = true
		// mode=1777 (como el /tmp de cualquier Linux): SiteUID no es el
		// dueño del tmpfs, necesita el bit "sticky" + escritura para todos.
		hostCfg.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,mode=1777,size=64m"}
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			m.network: {Aliases: []string{spec.Name}},
		},
	}

	resp, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("creando contenedor %s: %w", spec.Name, err)
	}
	return resp.ID, nil
}

func (m *Manager) EnsureImage(ctx context.Context, ref string) error {
	_, err := m.cli.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}
	rc, err := m.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("descargando imagen %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc) // hay que drenar el stream para que termine
	return nil
}

func (m *Manager) Start(ctx context.Context, nameOrID string) error {
	return m.cli.ContainerStart(ctx, nameOrID, container.StartOptions{})
}

func (m *Manager) Stop(ctx context.Context, nameOrID string) error {
	timeout := 15
	return m.cli.ContainerStop(ctx, nameOrID, container.StopOptions{Timeout: &timeout})
}

func (m *Manager) Restart(ctx context.Context, nameOrID string) error {
	timeout := 15
	return m.cli.ContainerRestart(ctx, nameOrID, container.StopOptions{Timeout: &timeout})
}

func (m *Manager) RemoveIfExists(ctx context.Context, name string) error {
	_, err := m.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return err
	}
	return m.cli.ContainerRemove(ctx, name, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: false,
	})
}

// State devuelve el estado ("running", "exited"…) del contenedor.
func (m *Manager) State(ctx context.Context, name string) (string, error) {
	info, err := m.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "missing", nil
		}
		return "", err
	}
	return info.State.Status, nil
}

// Logs devuelve las últimas líneas del contenedor (o un stream si follow).
func (m *Manager) Logs(ctx context.Context, name string, tail int, follow bool) (io.ReadCloser, error) {
	return m.cli.ContainerLogs(ctx, name, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     follow,
		Tail:       fmt.Sprint(tail),
	})
}

// Exec ejecuta un comando dentro de un contenedor de sitio (como SiteUID) y
// devuelve salida y código.
func (m *Manager) Exec(ctx context.Context, name string, cmd []string) (int, string, error) {
	return m.ExecEnv(ctx, name, cmd, nil)
}

// ExecEnv es como Exec pero permite pasar variables de entorno adicionales al
// proceso — lo usa el deploy por Git para fijar GIT_SSH_COMMAND sin tocar el
// entorno del contenedor en sí.
func (m *Manager) ExecEnv(ctx context.Context, name string, cmd []string, env []string) (int, string, error) {
	return m.ExecAs(ctx, name, cmd, SiteUID, env)
}

// ExecAs es la primitiva general: permite correr como un usuario distinto de
// SiteUID (usuario vacío = el que tenga por defecto el contenedor). La usan
// los backups para correr mysqldump dentro del contenedor de MariaDB, que no
// es un contenedor de sitio y no tiene por qué correr como SiteUID.
func (m *Manager) ExecAs(ctx context.Context, name string, cmd []string, user string, env []string) (int, string, error) {
	created, err := m.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
	})
	if err != nil {
		return -1, "", err
	}
	att, err := m.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return -1, "", err
	}
	defer att.Close()

	var sb strings.Builder
	sc := bufio.NewScanner(att.Reader)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		sb.WriteString(stripDockerFrame(sc.Text()))
		sb.WriteByte('\n')
	}

	insp, err := m.cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return -1, sb.String(), err
	}
	return insp.ExitCode, sb.String(), nil
}

// WriteFile sube un único archivo al contenedor sin pasar por el bind mount
// de /app — se usa para colocar de forma efímera la clave SSH de deploy
// antes de un git pull y borrarla justo después (vía Exec).
func (m *Manager) WriteFile(ctx context.Context, name, destPath string, content []byte, mode int64) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: strings.TrimPrefix(destPath, "/"),
		Mode: mode,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(content); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return m.cli.CopyToContainer(ctx, name, "/", &buf, container.CopyToContainerOptions{})
}

// ReadFile trae de vuelta un único archivo del contenedor como bytes
// crudos (a diferencia de Exec/ExecAs, que capturan la salida línea por
// línea y corromperían contenido binario como un dump comprimido). Se usa
// para bajar el resultado de mysqldump del contenedor de MariaDB.
func (m *Manager) ReadFile(ctx context.Context, name, srcPath string) ([]byte, error) {
	rc, _, err := m.cli.CopyFromContainer(ctx, name, srcPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	if _, err := tr.Next(); err != nil {
		return nil, fmt.Errorf("leyendo %s del contenedor: %w", srcPath, err)
	}
	return io.ReadAll(tr)
}

// Stats toma una muestra puntual de CPU/memoria/red del contenedor.
type Stats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
	NetRxMB    float64 `json:"net_rx_mb"`
	NetTxMB    float64 `json:"net_tx_mb"`
}

func (m *Manager) Stats(ctx context.Context, name string) (*Stats, error) {
	resp, err := m.cli.ContainerStatsOneShot(ctx, name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	s := &Stats{}
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		cpus := float64(raw.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpus == 0 {
			cpus = 1
		}
		s.CPUPercent = (cpuDelta / sysDelta) * cpus * 100.0
	}
	s.MemoryMB = float64(raw.MemoryStats.Usage) / (1024 * 1024)
	for _, n := range raw.Networks {
		s.NetRxMB += float64(n.RxBytes) / (1024 * 1024)
		s.NetTxMB += float64(n.TxBytes) / (1024 * 1024)
	}
	return s, nil
}

// ListManaged devuelve los contenedores creados por el panel.
func (m *Manager) ListManaged(ctx context.Context) ([]container.Summary, error) {
	return m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", LabelManaged+"=true")),
	})
}

// PublishedPort es un puerto que un contenedor en marcha publica al host
// (columna "ports" de `docker ps`, tal como quedó tras aplicar el
// docker-compose.yml — nunca hardcodeado en Go).
type PublishedPort struct {
	HostPort int
	Proto    string // "tcp" | "udp"
}

// PublishedPorts devuelve los puertos que el contenedor "name" tiene
// publicados al host ahora mismo (vacío si no existe o no está corriendo).
// Sirve para detectar puertos que ya quedaron abiertos porque Docker les
// puso su propia regla de iptables al publicarlos con "ports:" — eso pasa
// *antes* que el firewall del host (ufw) los vea, así que un puerto puede
// estar realmente accesible aunque ufw no tenga ninguna regla ALLOW para él.
func (m *Manager) PublishedPorts(ctx context.Context, name string) ([]PublishedPort, error) {
	info, err := m.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.State == nil || !info.State.Running {
		return nil, nil
	}
	var out []PublishedPort
	for portProto, bindings := range info.NetworkSettings.Ports {
		for _, b := range bindings {
			hp, err := strconv.Atoi(b.HostPort)
			if err != nil || hp == 0 {
				continue
			}
			out = append(out, PublishedPort{HostPort: hp, Proto: portProto.Proto()})
		}
	}
	return out, nil
}

// WaitHealthy espera a que el contenedor esté en estado "running".
func (m *Manager) WaitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := m.State(ctx, name)
		if err != nil {
			return err
		}
		if st == "running" {
			return nil
		}
		if st == "exited" || st == "dead" {
			return fmt.Errorf("el contenedor %s terminó con estado %s", name, st)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout esperando a que %s arranque", name)
}

// stripDockerFrame elimina la cabecera de 8 bytes del multiplexado de Docker
// cuando el contenedor no tiene TTY.
func stripDockerFrame(line string) string {
	if len(line) >= 8 && (line[0] == 1 || line[0] == 2) && line[1] == 0 {
		return line[8:]
	}
	return line
}

func ptr[T any](v T) *T { return &v }
