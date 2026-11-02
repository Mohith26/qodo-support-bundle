# Qodo Scout publication

Qodo Scout uses an authenticated GitHub release as the source of truth, a
development GCS canary, and a manually approved production promotion. The
workflows never rebuild during publication or promotion.

## Object contract

Each immutable release directory contains:

```text
support-bundle/releases/<version>/
  qodo-support-bundle-linux-amd64
  qodo-support-bundle-linux-arm64
  qodo-support-bundle-darwin-amd64
  qodo-support-bundle-darwin-arm64
  qodo-support-bundle-windows-amd64.exe
  qodo-support-bundle-windows-arm64.exe
  checksums.sha256
  install.sh
  install.ps1
  installer-checksums.sha256
```

`checksums.sha256` covers only the six binaries consumed by the installers.
`installer-checksums.sha256` separately covers the two installer scripts, so
there is no circular self-verification. All ten versioned objects use
`Cache-Control: public, max-age=31536000, immutable`. Binaries use
`application/octet-stream`; manifests and scripts use
`text/plain; charset=utf-8`.

The supported production paths are version-specific:

```text
https://get.qodo.ai/support-bundle/releases/<version>/<asset>
```

A separate discovery layer lets customers install without selecting a version:

```text
https://get.qodo.ai/support-bundle/install.sh
https://get.qodo.ai/support-bundle/install.ps1
https://get.qodo.ai/support-bundle/latest-version.txt
```

Only these three root objects are mutable. They use `Cache-Control: no-store`.
The version pointer contains one stable numeric version and a newline. Installers
resolve it once, then fetch the binary and checksum from the immutable directory.
Explicit version and offline installation retain the existing pinned contract.

## Release and publication sequence

1. The release workflow builds six binaries, copies the exact repository
   installer bytes, creates separate binary and installer manifests, attests
   all ten files, and uploads only missing GitHub release assets.
2. An existing same-name GitHub asset is downloaded and compared. Identical
   bytes make a rerun succeed; different bytes stop the release. Assets are
   never overwritten with `--clobber`.
3. A manual dev-publication dispatch verifies every release attestation and
   checksum before using the development publisher identity.
4. The dev publisher creates or verifies only the ten immutable versioned
   objects.
5. Dev canaries install from exact objects and run only `qodo-scout version`.
   The Windows ARM64 canary executes natively on `windows-11-arm`.
6. The production workflow requires the `production` environment approval. It
   independently verifies the GitHub release and exact dev objects before
   promoting byte-identical objects.
7. Production creates or verifies only immutable versioned objects. Public CDN
   canaries use versioned installers with an explicit version and run only
   `version`.

8. After all pinned production canaries succeed, the protected `publish-latest`
   job rechecks the authenticated release against production objects, rejects
   legacy installers without latest support, and refuses a version downgrade.
   Prerelease tags and GitHub releases marked prerelease or draft do not update latest.
9. Discovery publication conditionally updates both installer aliases, verifies
   them, then commits `latest-version.txt` last.
10. CDN verification checks all three discovery objects and their no-store headers.
    Latest-mode canaries exercise macOS, Linux, Windows AMD64 and Windows ARM64.

Neither pinned nor latest canaries start cluster collection.

## Operator dispatch checklist

1. Confirm the intended commit is on `main`, the version follows the installer
   grammar, and the matching GitHub release is published.
2. Let `.github/workflows/release-support-bundle.yaml` process the published
   release. A manual release run is recovery-only: run it from the matching tag
   ref and provide that same tag, never from an unrelated branch.
3. After the release assets, attestations, and native smoke jobs pass, manually
   dispatch `.github/workflows/publish-support-bundle.yaml` from `main` with the
   release version (for example `1.2.3`; the workflow also resolves its matching
   `v1.2.3` tag).
4. Review all exact dev object validation and pinned canary evidence. Stop on
   any mismatch.
5. Manually dispatch `.github/workflows/promote-support-bundle.yaml` from
   `main` with the same version. A named reviewer must approve the protected
   `production` environment before promotion can run.
6. Review exact GitHub/dev/CDN byte identity, headers, and every public canary.
   Preserve the workflow URLs, checksums, object generations, and approver in
   the release record.

Workflow dispatch, environment approval, and cloud credentials are live
operational actions. This runbook documents them but does not authorize them.

## Concurrency, reruns, and rollback

Versioned objects are create-only with generation-match `0`. Identical bytes
and headers are accepted after an exact-object read; any byte or header
conflict fails. Immutable publication uses exact object requests and requires no bucket listing
or overwrite permission. Discovery publication additionally needs permission to
replace only the three root discovery objects (Cloud Storage replacement requires
`storage.objects.create` and `storage.objects.delete`). Scope the production
identity accordingly with an IAM condition; do not grant overwrite rights to
`support-bundle/releases/`. Provision this permission before the first latest
publication. This change does not provision IAM permissions.

Workflow concurrency prevents overlapping attempts in the same environment.
The concurrency group covers the entire workflow, including discovery publication
and canaries. Discovery writes use the generation read from each object, so a
conflicting write fails rather than silently replacing a concurrent update.
No GCS publication lock is needed. A stale control object from an earlier publication model
is inert and must not be read, updated, or deleted by these workflows.

Rollback never mutates an immutable release. If a release must be replaced,
create and verify a newer version with known-good bytes, advance latest through
the same checks, and preserve prior objects as audit evidence. Customers may
explicitly pin an older release while that replacement is prepared. The root installers are version-independent bootstraps and may come from
separate releases during a partial publication; both resolve the same pointer.
An alias write failure leaves the prior pointer intact; rerunning retries the
publication. No root installer checksum manifest is published because a script
and separate manifest cannot be replaced atomically. For installer byte pinning
and checksum verification, use the immutable versioned scripts and their
versioned `installer-checksums.sha256`, or verify the downloaded script's GitHub
attestation.
A CDN or latest-canary failure after pointer commit fails the workflow and needs
operator investigation; it does not automatically downgrade the pointer.

### Recover an incomplete discovery publication

If a discovery write or verification fails, the script reports incomplete
publication and exits unsuccessfully. The root installers can temporarily have
different bytes. An installer-alias failure occurs before the pointer update;
a pointer verification failure can occur after its write succeeded. Do not
assume that a failed job means the pointer is unchanged.

1. Inspect the failed job, the three root objects, and their generations. Check
   whether `latest-version.txt` still selects the previous version, selects the
   attempted version, or was advanced by another publisher.
2. Resolve the reported permission, network, or object-verification failure.
   Keep customer docs on pinned installation until the first rollout completes.
3. If no newer version has been published, rerun the failed production workflow
   jobs for the same approved version. Publication revalidates the immutable
   release, reconciles both aliases, and only then writes and verifies the pointer.
4. If another publisher advanced the pointer, investigate that release's workflow
   and recover using its approved version. Do not delete objects or force an older
   pointer. The generation checks and downgrade guard must remain enabled.
5. Require successful CDN verification and all latest canaries before declaring
   recovery complete or publishing the versionless customer instructions.

Automatic compensating writes could overwrite a concurrent publisher or fail
for the same reason as the original write. Recovery therefore uses the existing
conditional, idempotent publication path instead of an automatic rollback.

## Discovery rehearsal

`go test ./internal/distribution -run '^TestLatest'` rehearses publication through
the real script with a fake GCS transport, without cloud credentials. It covers
partial-write recovery, pointer conflicts, and an isolated development bucket
and prefix with no production requests. Installer suites separately exercise
versionless resolution through fake network transports.

This rehearsal does not validate live development IAM or a CDN. The live dev
canaries still install pinned releases. Production discovery requires its own
protected approval and public canaries; do not treat the local rehearsal as
proof that production endpoints are available.

## Manual boundaries

Merging code does not publish anything. The following remain explicit operator
actions:

- complete and approve the
  [public distribution readiness checklist](public-readiness.md) before a
  customer pilot;
- dispatch dev publication from `main`;
- review dev canary evidence;
- approve the protected production environment;
- dispatch production promotion and review public canaries.

The workflows and scripts operate only under `support-bundle/`. Repository
visibility, platform signing, customer cloud-auth plugins, and automatic
cluster collection are outside this publication contract.

## First rollout

Release a new version containing the latest-capable installers, then run dev
publication and production promotion. Do not bootstrap latest by promoting an
old release whose installers require a version. The stable URLs are not made
live by merging this code. Merge the customer docs change only after the public
latest URLs and all latest-mode canaries pass. No per-release docs update is
needed after that.
