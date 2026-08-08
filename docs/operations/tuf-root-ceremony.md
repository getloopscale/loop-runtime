---
title: Offline TUF Root Ceremony
generated: { by: process:tenant-build-pipeline-gaps-closure-plan-t1.5, at: 2026-05-27 }
name: tuf-root-ceremony
description: Offline TUF root signing ceremony procedure (3-of-5 quorum, hardware-backed, air-gapped) per ADR-2026-05-27-T2.2-OFFLINE-TUF. Mandatory before Sigstore activation OR annual rotation.
type: operational-runbook
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: tenant-build-pipeline-gaps-closure plan T1.5
---

# Offline TUF Root Ceremony

> **Per ADR-2026-05-27-T2.2-OFFLINE-TUF + plan T1.5.** Multi-party, hardware-backed, air-gapped procedure for generating + rotating TUF root keys for LoopZero self-hosted Sigstore.
>
> **WARNING — DO NOT skip steps.** TUF root key compromise = supply chain compromise (retroactively forgeable via CTLog). 3-of-5 quorum + offline ceremony is the ONLY acceptable procedure per SBR conditional veto in MEET-2026-05-27.

## When to run

1. **Pre-Sigstore-activation** — first ceremony before `pulumi up` provisions Sigstore stack
2. **Annual rotation** — TUF root expires 365d post-ceremony; rotate at T+330d (30-day buffer before expiry)
3. **Emergency rotation** — if quorum member's hardware key compromised/lost (single member loss tolerable, 2+ requires fresh ceremony)

## Prerequisites

### Personnel — 5 quorum members

5 individuals with:
- Awareness of supply-chain security implications
- Available for ~3h ceremony session
- Trusted (background-checked OR long-term employees) — root key holders ARE the trust anchor
- 3 must be reachable for any future signing operation (quorum 3-of-5)

### Hardware — per member

- **YubiKey FIPS series 5** (NOT consumer YubiKey — FIPS variant required for SLSA L3)
- Each member personally owns their YubiKey (never shared, never reused)
- Backup: each member also has 1 backup YubiKey (kept in physical safe)

### Software — on air-gapped machine

- Clean Linux laptop (Ubuntu LTS recommended; fresh install, full-disk encryption)
- **NEVER connected to network** (post-OS-install: physically disable WiFi + Ethernet)
- Tools installed via USB:
  - `tuf-cli` v1.10+ (https://github.com/theupdateframework/go-tuf)
  - `sigstore-cli` v2.2+
  - `ykman` (YubiKey manager) for hardware key operations
  - GPG for output signing

### Physical environment

- Secure room (camera-monitored, badge-restricted entry)
- All members physically present (NO remote participation)
- Sterile workspace — no personal devices in room

## Procedure

### Phase 1 — Pre-ceremony preparation (T-7 days)

1. **Confirm 5 quorum members** + 5 YubiKeys + 5 backups available
2. **Provision air-gapped laptop:**
   - Fresh Ubuntu LTS install
   - Full-disk encryption (LUKS, ≥30-char passphrase split among 2 members)
   - Disable WiFi + Bluetooth at BIOS level
   - Physically disconnect Ethernet
   - Boot from USB with tuf-cli + sigstore-cli pre-installed
3. **Reserve secure room** for 3h window
4. **Distribute attendance reminder** with prep checklist

### Phase 2 — Ceremony execution (~3h)

#### Step 2.1 — Setup (15min)

- All 5 members physically present
- Power on air-gapped laptop, verify network DISCONNECTED (no IP):
  ```bash
  ip a  # should show only lo (loopback)
  ```
- Mount fresh empty USB for ceremony output
- Initialize ceremony working directory:
  ```bash
  mkdir -p ~/tuf-ceremony-$(date +%Y%m%d)
  cd ~/tuf-ceremony-$(date +%Y%m%d)
  ```

#### Step 2.2 — Generate TUF root (15min)

```bash
# Initialize TUF root metadata (no keys yet)
tuf-cli init \
  --consistent-snapshot \
  --expires-in 365d \
  > root.json.unsigned
```

Verify output is valid JSON + expiry 365d ahead.

#### Step 2.3 — Each member signs with YubiKey (60min — 12min/member)

For each member (1 → 5):

1. Member inserts personal YubiKey
2. Member authenticates via YubiKey PIN (private — typed away from cameras)
3. Member signs root metadata:
   ```bash
   ykman piv keys generate 9c pubkey-${MEMBER_NAME}.pem
   ykman piv certificates generate 9c pubkey-${MEMBER_NAME}.pem
   tuf-cli sign root.json.unsigned \
     --key-source yubikey:9c \
     --key-id $MEMBER_NAME
   ```
4. Member exports public key (for later verification):
   ```bash
   ykman piv keys export 9c pubkey-${MEMBER_NAME}.pem
   ```
5. Member's signature appended to root.json.signed
6. **Verify** signature added: `tuf-cli verify root.json.signed`

After all 5: root.json.signed has 5 signatures (3-of-5 quorum verifiable).

#### Step 2.4 — Output sealing (30min)

1. **Compute SHA-256 hash** of root.json.signed (each member reads aloud + records on paper)
2. **Bundle ceremony output:**
   ```bash
   mkdir output
   cp root.json.signed output/
   cp pubkey-*.pem output/
   echo "Ceremony date: $(date -u +%Y-%m-%dT%H:%M:%SZ)" > output/ceremony-metadata.txt
   echo "Members: alice bob carol dan eve" >> output/ceremony-metadata.txt
   echo "Expiry: $(date -u -d '+365 days' +%Y-%m-%dT%H:%M:%SZ)" >> output/ceremony-metadata.txt
   sha256sum output/root.json.signed > output/SHA256SUMS
   ```
3. **GPG-sign the bundle** (each member contributes signature)
4. **Copy to ceremony USB** (encrypted volume)
5. **Lock laptop** + power down

#### Step 2.5 — USB distribution (15min)

- Primary USB → secure vault (operator access only)
- Backup USB → secondary vault (separate physical location)
- Each member retains personal YubiKey + backup (NEVER leave the room with both — backup goes to personal safe at home/office)

### Phase 3 — Post-ceremony Kubernetes setup (~30min, operator-only)

1. **Mount primary USB** on operator workstation (with network)
2. **Verify SHA-256** of root.json.signed matches ceremony record
3. **Create Kubernetes Secret** in mgmt cluster:
   ```bash
   kubectl create namespace sigstore-system --dry-run=client -o yaml | kubectl apply -f -

   kubectl create secret generic sigstore-tuf-ceremony \
     --from-file=root.json=/media/usb/output/root.json.signed \
     --from-file=pubkeys=/media/usb/output/ \
     --namespace=sigstore-system
   ```
4. **Configure Pulumi stack** with tufPreMintedKeys reference:
   ```bash
   pulumi config set --stack mgmt sigstore:tufPreMintedKeys.secretName sigstore-tuf-ceremony
   pulumi config set --stack mgmt sigstore:tufPreMintedKeys.ceremonyDate "$(jq -r .ceremonyDate /media/usb/output/ceremony-metadata.txt)"
   ```
5. **Provision Sigstore stack:**
   ```bash
   cd infra/pulumi-platform
   pulumi up --stack mgmt
   # Wait ~20min for TUF bootstrap + ACME issuance
   ```
6. **Set Helm value** for TUF expiry CronJob (T1.2.b):
   ```bash
   helmfile -e prod sync --set tufRootCeremonyDate="$(pulumi stack output --stack mgmt tufCeremonyDate)"
   ```
7. **Verify** TUF expiry CronJob fires + Prometheus metric appears:
   ```bash
   kubectl get cronjob -n sigstore-system tuf-expiry-emitter
   kubectl create job --from=cronjob/tuf-expiry-emitter manual-test -n sigstore-system
   kubectl logs -n sigstore-system -l job=manual-test
   # Check Pushgateway has metric
   curl -s http://prometheus-pushgateway.monitoring:9091/metrics | grep tuf_root_expiry_timestamp
   ```

### Phase 4 — Rotation calendar (operator)

- **T+335d:** Schedule next ceremony (book secure room, confirm members availability)
- **T+350d:** Send T-15d reminder to members
- **T+358d:** Send T-7d reminder
- **T+360d:** TUF expiry CRITICAL alert fires (5 days remaining)
- **T+365d:** Hard expiry — Sigstore signing chain breaks if rotation not completed

## Failure modes

### Member absent at ceremony

- Reschedule (max 30 days delay before T-30d alert fires)
- If sustained absence: replace member (background check + key issuance)

### YubiKey lost/damaged

- **Single member loss:** 4-of-5 = quorum still valid (3-of-5 requirement met). Continue + schedule replacement at next rotation.
- **2+ member loss:** EMERGENCY — quorum at risk. Schedule emergency ceremony within 14d. Engage backup hardware keys.

### Air-gapped laptop network leak

- If laptop ever connected to network DURING ceremony: ABORT ceremony, wipe disk, restart from scratch on new laptop. Ceremony with potentially-exfiltrated key material is invalid.

### Output USB corruption

- Backup USB provides redundancy
- If BOTH USBs corrupted: re-ceremony required (root keys gone — no recovery)

## Compliance + audit trail

- **Recording:** ceremony video-recorded (faces blurred for PII compliance) — stored in encrypted S3 bucket with 7-year retention (SOC 2 + SLSA L3 audit window)
- **Members logbook:** physical paper log signed by all 5 — kept in vault
- **Pulumi outputs:** `pulumi stack output tufCeremonyDate` — Prometheus metric, retained indefinitely
- **Annual review:** operator + sponsor review ceremony log + members list at each rotation

## References

- ADR-2026-05-27-T2.2-OFFLINE-TUF: `.claude/knowledge-base/decisions/2026-05-27-T2.2-offline-tuf-ceremony.md`
- Plan T1.5: [`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`](../plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md)
- Sigstore canonical multi-party root signing: https://github.com/sigstore/root-signing
- TUF specification: https://theupdateframework.io/specification/latest/
- YubiKey FIPS docs: https://www.yubico.com/products/yubikey-fips/
- SLSA L3 requirements: https://slsa.dev/spec/v1.0/levels#build-l3
- Related runbooks: [`docs/operations/sigstore-storage-model.md`](./sigstore-storage-model.md)
