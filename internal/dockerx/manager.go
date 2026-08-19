// Package dockerx encapsula la orquestación de los contenedores FrankenPHP:
// uno por sitio, con límites de CPU/memoria y su raíz montada desde el host.
package dockerx

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		Image:  spec.Image,
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
		hostCfg.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,size=64m"}
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

// Exec ejecuta un comando dentro del contenedor y devuelve salida y código.
func (m *Manager) Exec(ctx context.Context, name string, cmd []string) (int, string, error) {
	created, err := m.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		User:         "www-data",
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

// Stats toma una muestra puntual de CPU/memoria/red del contenedor.
type Stats struct {
	CPUPercent float64
	MemoryMB   float64
	NetRxMB    float64
	NetTxMB    float64
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
