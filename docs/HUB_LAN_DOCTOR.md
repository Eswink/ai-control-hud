# Hub trusted-LAN doctor

The Hub LAN doctor is a read-only operational check for CentOS/RHEL. It was added after real-device validation found a configuration where the Go Hub and TCP API were healthy but Windows/Android auto-discovery failed because UDP `8788` was not allowed by firewalld.

The doctor **does not modify firewall rules or service configuration**.

## Preferred run after installation

The systemd installer deploys the doctor next to the Hub binary at:

```text
/usr/local/lib/ai-control-hub/ai-control-hub-lan-doctor.sh
```

For a Hub installed with `--lan-auto`, run:

```bash
bash scripts/ai-control-hub-systemd.sh lan-doctor
```

The adapter reads the installed unit and automatically passes its actual `HUD_HUB_HTTP_PORT` and `HUD_HUB_DISCOVERY_PORT` to the installed doctor. This avoids silently checking default ports after a custom discovery-port installation.

`lan-doctor` intentionally rejects a unit where `HUD_HUB_DISCOVERY_ENABLED` is not `1`; it is a LAN-auto diagnostic, not a generic manual/loopback-mode health command.

## Direct doctor invocation

From an unpacked validation/deployment bundle, the underlying doctor remains directly usable:

```bash
bash scripts/ai-control-hub-lan-doctor.sh
```

Defaults:

```text
service          ai-control-hub.service
HTTP             TCP 8787
LAN discovery    UDP 8788
health           http://127.0.0.1:8787/api/v1/health
```

Optional overrides:

```bash
bash scripts/ai-control-hub-lan-doctor.sh \
  --http-port 8787 \
  --discovery-port 8788 \
  --health-url http://127.0.0.1:8787/api/v1/health \
  --service ai-control-hub.service
```

Use `--no-firewall` only when firewalld is intentionally not the host firewall and you want to check service/listeners/health separately.

## Checks

The doctor reports:

1. systemd service state when `systemctl` is available;
2. a TCP listener on the configured Hub HTTP port;
3. a UDP listener on the configured discovery port;
4. successful local Hub `/api/v1/health` access;
5. active firewalld zones;
6. runtime firewalld allowance for both configured TCP/UDP ports in at least one active zone;
7. permanent firewalld allowance for both ports so reload/reboot does not silently break LAN discovery.

It prints each active firewalld zone separately. On hosts with multiple network interfaces/zones, confirm that the passing zone is the one attached to the trusted LAN used by Windows and Android.

## Exit codes

Underlying doctor:

```text
0  service/listeners/health pass and firewalld checks pass (or firewalld is not active)
1  Hub runtime failure: service/listener/health failed
2  Hub appears healthy but LAN/firewall verification is incomplete or blocked
64 invalid doctor command-line arguments
```

The systemd adapter additionally returns `2` when the installed unit is not configured for LAN auto-discovery.

Exit `2` is intentionally distinct from a dead Hub. A common example is:

```text
TCP 8787 listener: PASS
UDP 8788 listener: PASS
/health: PASS
firewalld UDP 8788: missing
```

In that state direct HTTP can work while `auto://lan` / `http://auto.lan` discovery fails.

## Firewalld remediation

The doctor does not choose a zone or change policy automatically. After identifying the correct trusted-LAN zone, the operator can explicitly configure it, for example:

```bash
sudo firewall-cmd --zone=<LAN_ZONE> --permanent --add-port=8787/tcp
sudo firewall-cmd --zone=<LAN_ZONE> --permanent --add-port=8788/udp
sudo firewall-cmd --reload
```

Then rerun:

```bash
bash scripts/ai-control-hub-systemd.sh lan-doctor
```

Do not expose these ports on an untrusted/public zone merely to make the doctor pass.

## Limits

This script cannot prove end-to-end UDP broadcast delivery through an access point, VLAN, Wi-Fi client-isolation policy, hypervisor bridge, or upstream network ACL. It verifies the Hub host's local prerequisites and firewalld state only.
