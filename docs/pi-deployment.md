# Raspberry Pi Deployment

This document describes the recommended deployment setup for running `empirebusd` on a Raspberry Pi in the van and automatically deploying from GitHub whenever `main` changes.

The target host assumption for this guide is:

- Raspberry Pi 5
- Raspberry Pi OS or another `systemd`-based Linux distribution
- Tailscale installed and already connected to your tailnet
- deploys performed over Tailscale, not exposed to the public internet

## Recommended Architecture

Use:

- GitHub-hosted Actions runners for CI and deployment orchestration
- the official Tailscale GitHub Action so the workflow can join the tailnet
- Tailscale SSH from the workflow to the Pi
- a `systemd` service on the Pi
- immutable release directories under `/opt/xtura/releases/<git-sha>`
- a `current` symlink under `/opt/xtura/current`

This keeps CI off the Pi and keeps the Pi focused on running the service.

## Why Not A Self-Hosted Runner On The Pi

That setup works, but it is a worse fit for a van host:

- CI and production become the same machine
- the Pi needs GitHub runner maintenance
- builds consume resources on the prod host
- a rebooted or offline Pi blocks the pipeline

For this repo, the GitHub-hosted runner plus Tailscale deploy path is the better default.

## Files In This Repo

- workflow: [.github/workflows/deploy-pi.yml](/Users/rog/Development/xtura-automation/.github/workflows/deploy-pi.yml)
- remote deploy script: [scripts/deploy/deploy-on-host.sh](/Users/rog/Development/xtura-automation/scripts/deploy/deploy-on-host.sh)
- `systemd` unit: [ops/systemd/empirebusd.service](/Users/rog/Development/xtura-automation/ops/systemd/empirebusd.service)

## Pi Directory Layout

Recommended layout on the Pi:

- binary releases: `/opt/xtura/releases/<git-sha>/empirebusd`
- active symlink: `/opt/xtura/current`
- live config: `/var/lib/xtura/config.yaml`
- runtime mode state: `/var/lib/xtura/config.yaml.runtime.yaml`

The service writes schedule updates back to the configured YAML file and writes runtime mode state next to it, so the config must live in a writable location. That is why this guide uses `/var/lib/xtura/` instead of `/etc/xtura/`.

## One-Time Pi Setup

Before using the commands below, get a local checkout of this repo onto the Pi once so the sample config, `systemd` unit, and deploy script are available during bootstrap. Do this as your normal admin/deploy user, not as the `xtura` service user.

Example:

```bash
mkdir -p ~/src
cd ~/src
git clone <YOUR_GITHUB_REPO_URL> xtura-automation
cd xtura-automation
```

If you prefer not to clone on the Pi, you can copy just these files there by some other means:

- `config.example.yaml`
- `ops/systemd/empirebusd.service`
- `scripts/deploy/deploy-on-host.sh`

Install system dependencies:

```bash
sudo apt-get update
sudo apt-get install -y curl tar
```

Create the service user and directories:

```bash
sudo useradd --system --home /opt/xtura --shell /usr/sbin/nologin xtura || true
sudo mkdir -p /opt/xtura/releases /var/lib/xtura
sudo chown -R xtura:xtura /opt/xtura /var/lib/xtura
```

Create the live config from the sample:

```bash
sudo install -m 0644 /dev/stdin /var/lib/xtura/config.yaml < config.example.yaml
sudo chown xtura:xtura /var/lib/xtura/config.yaml
```

Then edit `/var/lib/xtura/config.yaml` for the Pi environment:

- set the Garmin websocket URL
- set the desired API listen address and port
- set the real heating schedule

Install the service unit:

```bash
sudo install -m 0644 ops/systemd/empirebusd.service /etc/systemd/system/empirebusd.service
sudo systemctl daemon-reload
sudo systemctl enable empirebusd.service
```

Do not start it yet if you have not done the first deploy, because `/opt/xtura/current/empirebusd` will not exist yet.

## Tailscale Setup

The Pi should already be on Tailscale.

Recommended:

- give the Pi a stable Tailscale DNS name
- enable Tailscale SSH on the Pi
- allow the GitHub Actions ephemeral node tag to SSH to the deploy user on the Pi

Typical shape in your tailnet policy:

- source: `tag:XTURA-CI`
- destination: the Pi host
- user: your deploy user, for example `rog` or another sudo-capable admin account

The GitHub workflow uses the official Tailscale GitHub Action, which creates an ephemeral node inside your tailnet during the workflow run:

- [Tailscale GitHub Action](https://tailscale.com/docs/integrations/github/github-action)
- [Tailscale SSH](https://tailscale.com/docs/features/tailscale-ssh)

## GitHub Repository Setup

Set these repository variables:

- `PI_HOST`
  Recommended value: `jones-pi.taile19bc2.ts.net`
- `PI_USER`
  Example: `rog`
- `PI_PORT`
  Optional. Default is `22`.

Set these repository secrets:

- `TS_OAUTH_CLIENT_ID`
- `TS_OAUTH_SECRET`

Those come from your Tailscale OAuth client setup.

Recommended GitHub environment:

- create a `production` environment
- optionally require approval for deploys
- optionally restrict deploys to `main`

Relevant GitHub docs:

- [Deployments with GitHub Actions](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/control-deployments)
- [Managing environments](https://docs.github.com/en/actions/reference/environments)

## How Deploys Work

On push to `main` or manual dispatch, the workflow:

1. checks out the repo
2. installs Go
3. runs `go test ./...`
4. builds a static `linux/arm64` binary for Pi 5
5. joins the tailnet with the Tailscale GitHub Action
6. copies the artifact, deploy script, and service file to the Pi
7. runs the deploy script on the Pi with `sudo`
8. restarts `empirebusd`
9. checks `http://127.0.0.1:8080/v1/health` on the Pi

## Deploy Script Behavior

The remote script:

- creates a new release directory under `/opt/xtura/releases/<git-sha>`
- extracts the built artifact there
- optionally updates the `systemd` unit file
- flips `/opt/xtura/current` to the new release
- runs `systemctl daemon-reload`
- enables the service
- restarts the service

The live config in `/var/lib/xtura/config.yaml` is never overwritten by deploys.

## Manual Verification

After the first successful deploy, check on the Pi:

```bash
systemctl status empirebusd.service
curl http://127.0.0.1:8080/v1/health
journalctl -u empirebusd.service -n 100 --no-pager
```

## Rollback

To roll back manually on the Pi:

```bash
sudo ln -sfn /opt/xtura/releases/<older-sha> /opt/xtura/current
sudo systemctl restart empirebusd.service
```

## Notes

- This workflow assumes `linux/arm64`, which matches a Pi 5. If you later move to a different Pi model, update the `GOARCH` in the workflow.
- The workflow currently uses `curl` against `127.0.0.1:8080`. If you change the service port, update the health check in the workflow.
- The deploy script expects the deploy user to have `sudo` rights on the Pi.
