package distribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestPublishesDiscoveryOnlyAndCommitsPointerLast(t *testing.T) {
	fixture := newLatestFixture(t)

	output, err := fixture.command("1.2.3").CombinedOutput()

	if err != nil {
		t.Fatalf("publication failed: %v\n%s", err, output)
	}
	writes := []string{}
	for _, line := range strings.Split(readText(t, fixture.requests), "\n") {
		if !strings.HasPrefix(line, "PUT|") {
			continue
		}
		fields := strings.Split(line, "|")
		if fields[3] != "no-store" || fields[4] != "0" {
			t.Fatalf("unexpected discovery write: %s", line)
		}
		writes = append(writes, strings.TrimPrefix(fields[1], "https://storage.googleapis.com/qodo-cli-public/support-bundle/"))
	}
	if strings.Join(writes, ",") != "install.sh,install.ps1,latest-version.txt" {
		t.Fatalf("unexpected publication order or scope: %v", writes)
	}
	if readText(t, fixture.object("latest-version.txt")) != "1.2.3\n" {
		t.Fatal("latest pointer did not select the verified version")
	}
}

func TestLatestAdvancesExistingPointerAndAllowsRetry(t *testing.T) {
	fixture := newLatestFixture(t)
	writeLatestFile(t, fixture.object("latest-version.txt"), "1.2.2\n")

	firstOutput, firstErr := fixture.command("1.2.3").CombinedOutput()
	secondOutput, secondErr := fixture.command("1.2.3").CombinedOutput()

	if firstErr != nil || secondErr != nil {
		t.Fatalf("publication/retry failed: %v / %v\n%s\n%s", firstErr, secondErr, firstOutput, secondOutput)
	}
	if readText(t, fixture.object("latest-version.txt")) != "1.2.3\n" {
		t.Fatal("latest pointer was not advanced")
	}
}

func TestLatestRejectsOlderOrPrereleaseVersionsBeforeWriting(t *testing.T) {
	for _, version := range []string{"1.2.3", "1.2.4-beta", "01.2.4", "1.2.4\n1.2.5"} {
		t.Run(version, func(t *testing.T) {
			fixture := newLatestFixture(t)
			writeLatestFile(t, fixture.object("latest-version.txt"), "1.10.0\n")

			output, err := fixture.command(version).CombinedOutput()

			if err == nil {
				t.Fatalf("accepted older or unstable release: %s", output)
			}
			if readText(t, fixture.object("latest-version.txt")) != "1.10.0\n" {
				t.Fatal("changed latest on rejected publication")
			}
			requests, _ := os.ReadFile(fixture.requests)
			if strings.Contains(string(requests), "PUT|") {
				t.Fatalf("rejected publication wrote discovery objects: %s", requests)
			}
		})
	}
}

func TestLatestPreservesPointerWhenDiscoveryWriteFails(t *testing.T) {
	for _, name := range []string{"install.sh", "install.ps1", "latest-version.txt"} {
		t.Run(name, func(t *testing.T) {
			fixture := newLatestFixture(t)
			writeLatestFile(t, fixture.object("latest-version.txt"), "1.2.2\n")

			output, err := fixture.command("1.2.3", "FAKE_FAIL_OBJECT=qodo-cli-public/support-bundle/"+name).CombinedOutput()

			if err == nil {
				t.Fatalf("accepted failed write: %s", output)
			}
			if readText(t, fixture.object("latest-version.txt")) != "1.2.2\n" {
				t.Fatal("failed discovery write advanced latest")
			}
		})
	}
}

func TestLatestVerifiesImmutableReleaseBeforePublishing(t *testing.T) {
	fixture := newLatestFixture(t)
	writeLatestFile(t, fixture.object("releases/1.2.3/install.sh"), "tampered")

	output, err := fixture.command("1.2.3").CombinedOutput()

	if err == nil {
		t.Fatalf("accepted tampered release: %s", output)
	}
	if strings.Contains(readText(t, fixture.requests), "PUT|") {
		t.Fatal("wrote discovery before verifying production release")
	}
}

func TestLatestRejectsLegacyBootstrapInstallers(t *testing.T) {
	fixture := newLatestFixture(t)
	writeReleaseFixture(t, fixture.release, "legacy installer")

	output, err := fixture.command("1.2.3").CombinedOutput()

	if err == nil || !strings.Contains(string(output), "does not support latest discovery") {
		t.Fatalf("legacy installer was not rejected: %v\n%s", err, output)
	}
	if _, err := os.Stat(fixture.requests); !os.IsNotExist(err) {
		t.Fatal("legacy installer validation contacted storage")
	}
}

func TestLatestPublicationWaitsForEveryPinnedProductionCanary(t *testing.T) {
	workflow := readText(t, filepath.Join(repositoryRoot(t), ".github/workflows/promote-support-bundle.yaml"))

	latestJob := strings.SplitN(workflow, "\n  publish-latest:", 2)

	if len(latestJob) != 2 || !strings.Contains(latestJob[1], "needs: [promote, production-unix-canary, production-windows-arm64-canary]") {
		t.Fatal("latest publication is not gated on all production canaries")
	}
	if !strings.Contains(latestJob[1], "environment: production") || !strings.Contains(workflow, "cancel-in-progress: false") {
		t.Fatal("latest publication lacks production approval or serialization")
	}
	if !strings.Contains(latestJob[1], "verify-production-cdn.sh --latest") || !strings.Contains(latestJob[1], "canary-production-installers.sh --latest") {
		t.Fatal("public discovery is not verified and exercised")
	}
}

func TestLatestConcurrentPointerUpdateIsPreserved(t *testing.T) {
	fixture := newLatestFixture(t)
	writeLatestFile(t, fixture.object("latest-version.txt"), "1.2.2\n")

	output, err := fixture.command("1.2.3", "FAKE_RACE_LATEST_VERSION=1.2.4").CombinedOutput()

	if err == nil {
		t.Fatalf("stale publication replaced the concurrent version: %s", output)
	}
	if readText(t, fixture.object("latest-version.txt")) != "1.2.4\n" {
		t.Fatal("concurrent latest pointer was not preserved")
	}
}

func TestLatestRetryReconcilesPartialBootstrapPublication(t *testing.T) {
	fixture := newLatestFixture(t)
	writeLatestFile(t, fixture.object("latest-version.txt"), "1.2.2\n")
	writeLatestFile(t, fixture.object("install.sh"), "previous Unix installer")
	writeLatestFile(t, fixture.object("install.ps1"), "previous Windows installer")

	output, err := fixture.command("1.2.3", "FAKE_FAIL_OBJECT=qodo-cli-public/support-bundle/install.ps1").CombinedOutput()

	if err == nil || !strings.Contains(string(output), "discovery publication is incomplete") ||
		!strings.Contains(string(output), "rerun the same approved version") {
		t.Fatalf("missing recovery diagnostic: %v\n%s", err, output)
	}
	if readText(t, fixture.object("latest-version.txt")) != "1.2.2\n" ||
		readText(t, fixture.object("install.sh")) != readText(t, filepath.Join(fixture.release, "install.sh")) ||
		readText(t, fixture.object("install.ps1")) != "previous Windows installer" {
		t.Fatal("second-alias failure did not leave the expected partial publication")
	}

	output, err = fixture.command("1.2.3").CombinedOutput()

	if err != nil || readText(t, fixture.object("latest-version.txt")) != "1.2.3\n" ||
		readText(t, fixture.object("install.sh")) != readText(t, filepath.Join(fixture.release, "install.sh")) ||
		readText(t, fixture.object("install.ps1")) != readText(t, filepath.Join(fixture.release, "install.ps1")) {
		t.Fatalf("retry did not reconcile both aliases and commit the pointer: %v\n%s", err, output)
	}
}

func TestLatestRehearsalUsesOnlyConfiguredDevelopmentDestination(t *testing.T) {
	fixture := newLatestFixture(t)
	bucket, prefix := "qodo-cli-public-dev", "scout-rehearsal"
	copyReleaseFixture(t, fixture.release, filepath.Join(fixture.storage, bucket, prefix, "releases/1.2.3"))

	output, err := fixture.command("1.2.3", "QODO_SUPPORT_BUNDLE_DESTINATION_BUCKET="+bucket,
		"QODO_SUPPORT_BUNDLE_PREFIX="+prefix).CombinedOutput()

	if err != nil {
		t.Fatalf("development rehearsal failed: %v\n%s", err, output)
	}
	if readText(t, filepath.Join(fixture.storage, bucket, prefix, "latest-version.txt")) != "1.2.3\n" {
		t.Fatal("rehearsal did not publish the development pointer")
	}
	for _, request := range strings.Split(strings.TrimSpace(readText(t, fixture.requests)), "\n") {
		if !strings.Contains(request, "https://storage.googleapis.com/"+bucket+"/"+prefix+"/") {
			t.Fatalf("rehearsal requested an object outside its development destination: %s", request)
		}
	}
}
