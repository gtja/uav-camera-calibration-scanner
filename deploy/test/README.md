# Test Environment Deployment

This directory contains the runtime-only Docker Compose file for the test host.
It assumes the image has already been loaded or pulled on the target machine.

```bash
mkdir -p /opt/uav-camera-calibration-scanner/out/docker/{artifacts,results,run}
cp deploy/test/docker-compose.yml /opt/uav-camera-calibration-scanner/docker-compose.yml
cp configs/test-env-s220-20260708.yml /opt/uav-camera-calibration-scanner/config.yml
printf 'IMAGE=registry.jingan.com:32008/ja/uav-camera-calibration-scanner:v0.1.0.YYMMDD\n' > /opt/uav-camera-calibration-scanner/.env
chown -R 10001:10001 /opt/uav-camera-calibration-scanner/out/docker

cd /opt/uav-camera-calibration-scanner
docker-compose up -d
docker-compose logs -f --tail=100
```

The mounted `out/docker` directory must be writable by UID `10001`, because the
container runs as the non-root `app` user. This directory stores the run lock,
property artifacts, and JSONL results used by the 7-day calibration de-dup rule.

When the host cannot pull from the registry, load a saved image first and put
that local tag in `.env`:

```bash
gzip -dc uav-camera-calibration-scanner-*.tar.gz | docker load
printf 'IMAGE=uav-camera-calibration-scanner:test-env-YYYYMMDD-HHMM\n' > /opt/uav-camera-calibration-scanner/.env
```
