package provision

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

const deployKeyContainerPath = "/tmp/gocp-deploy-key"

// gitSSHCommand desactiva la verificación de host: la clave privada es
// efímera dentro del contenedor (se sube justo antes del pull y se borra
// después), así que no tiene sentido persistir un known_hosts entre pulls.
const gitSSHCommand = "ssh -i " + deployKeyContainerPath +
	" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes"

// ConnectGit conecta (o actualiza) el repositorio Git de un sitio. La primera
// vez genera un par de claves SSH propio del sitio y un secreto de webhook;
// las siguientes solo actualiza repo/rama/auto-deploy y deja la clave tal
// como estaba.
func (s *Service) ConnectGit(ctx context.Context, siteID uuid.UUID, repoURL, branch string, autoDeploy bool) (*models.SiteGitConfig, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, ValidationError{Field: "repo_url", Message: "es obligatorio"}
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}

	if _, err := s.st.GetSite(ctx, siteID); err != nil {
		return nil, err
	}

	existing, err := s.st.GetSiteGitConfig(ctx, siteID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		if err := s.st.UpdateSiteGitConfig(ctx, siteID, repoURL, branch, autoDeploy); err != nil {
			return nil, err
		}
		return s.st.GetSiteGitConfig(ctx, siteID)
	}

	privatePEM, publicKey, err := generateDeployKey()
	if err != nil {
		return nil, fmt.Errorf("generando la clave de deploy: %w", err)
	}
	privateEnc, err := encryptPrivateKey(s.cfg.JWTSecret, privatePEM)
	if err != nil {
		return nil, fmt.Errorf("cifrando la clave de deploy: %w", err)
	}
	secret, err := randomWebhookSecret()
	if err != nil {
		return nil, fmt.Errorf("generando el secreto del webhook: %w", err)
	}

	cfg := &models.SiteGitConfig{
		SiteID: siteID, RepoURL: repoURL, Branch: branch,
		PublicKey: publicKey, PrivateKeyEnc: privateEnc,
		WebhookSecret: secret, AutoDeploy: autoDeploy,
	}
	if err := s.st.CreateSiteGitConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *Service) DisconnectGit(ctx context.Context, siteID uuid.UUID) error {
	return s.st.DeleteSiteGitConfig(ctx, siteID)
}

// GitDeploy trae los últimos cambios del repositorio configurado y los
// aplica dentro del contenedor ya corriendo (clone la primera vez, fetch +
// reset --hard después), corre composer si hace falta y reinicia el
// contenedor para que PHP/OPcache tomen el código nuevo.
func (s *Service) GitDeploy(ctx context.Context, siteID uuid.UUID) (string, error) {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return "", err
	}
	cfg, err := s.st.GetSiteGitConfig(ctx, siteID)
	if err != nil {
		return "", err
	}
	_ = s.st.RecordGitDeploy(ctx, siteID, "running", "")

	output, deployErr := s.runGitDeploy(ctx, site, cfg)
	if deployErr != nil {
		_ = s.st.RecordGitDeploy(ctx, siteID, "failed", output+"\n"+deployErr.Error())
		return output, deployErr
	}
	_ = s.st.RecordGitDeploy(ctx, siteID, "success", output)
	return output, nil
}

func (s *Service) runGitDeploy(ctx context.Context, site *models.Site, cfg *models.SiteGitConfig) (string, error) {
	privatePEM, err := decryptPrivateKey(s.cfg.JWTSecret, cfg.PrivateKeyEnc)
	if err != nil {
		return "", fmt.Errorf("descifrando la clave de deploy: %w", err)
	}
	if err := s.docker.WriteFile(ctx, site.ContainerName, deployKeyContainerPath, privatePEM, 0o644); err != nil {
		return "", fmt.Errorf("subiendo la clave de deploy al contenedor: %w", err)
	}
	defer s.docker.ExecEnv(ctx, site.ContainerName, []string{"rm", "-f", deployKeyContainerPath}, nil)

	env := []string{"GIT_SSH_COMMAND=" + gitSSHCommand}
	var out strings.Builder

	code, probe, err := s.docker.ExecEnv(ctx, site.ContainerName,
		[]string{"sh", "-c", "test -d /app/.git"}, nil)
	if err != nil {
		return probe, fmt.Errorf("comprobando el estado del repositorio: %w", err)
	}

	if code != 0 {
		clean := "find /app -mindepth 1 -maxdepth 1 -exec rm -rf {} +"
		cloneCmd := fmt.Sprintf("git clone --branch %s --single-branch %s /app",
			shellQuote(cfg.Branch), shellQuote(cfg.RepoURL))
		exit, o, err := s.docker.ExecEnv(ctx, site.ContainerName,
			[]string{"sh", "-c", clean + " && " + cloneCmd}, env)
		out.WriteString(o)
		if err != nil {
			return out.String(), err
		}
		if exit != 0 {
			return out.String(), fmt.Errorf("git clone terminó con código %d", exit)
		}
	} else {
		pullCmd := fmt.Sprintf("git fetch origin %s && git reset --hard origin/%s",
			shellQuote(cfg.Branch), shellQuote(cfg.Branch))
		exit, o, err := s.docker.ExecEnv(ctx, site.ContainerName,
			[]string{"sh", "-c", "cd /app && " + pullCmd}, env)
		out.WriteString(o)
		if err != nil {
			return out.String(), err
		}
		if exit != 0 {
			return out.String(), fmt.Errorf("git pull terminó con código %d", exit)
		}
	}

	exit, o, err := s.docker.Exec(ctx, site.ContainerName,
		[]string{"sh", "-c", "test -f /app/composer.json && composer install --no-interaction --no-dev --optimize-autoloader --working-dir=/app || true"})
	out.WriteString(o)
	if err != nil {
		return out.String(), fmt.Errorf("corriendo composer: %w", err)
	}
	if exit != 0 {
		return out.String(), fmt.Errorf("composer install terminó con código %d", exit)
	}

	if err := s.docker.Restart(ctx, site.ContainerName); err != nil {
		return out.String(), fmt.Errorf("reiniciando el contenedor: %w", err)
	}
	return out.String(), nil
}

// shellQuote envuelve un valor en comillas simples para pasarlo seguro a
// `sh -c`: el repo/rama vienen del usuario y no deben poder inyectar
// comandos adicionales.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
