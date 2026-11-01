package distribution

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type latestFixture struct {
	release, storage, requests, bin, root string
}

func newLatestFixture(t *testing.T) latestFixture {
	t.Helper()
	fixture := latestFixture{t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "requests"), installFakeGCSCommands(t), repositoryRoot(t)}
	writeReleaseFixture(t, fixture.release, "latest-version.txt")
	copyReleaseFixture(t, fixture.release, filepath.Join(fixture.storage, "qodo-cli-public/support-bundle/releases/1.2.3"))
	return fixture
}

func (fixture latestFixture) command(version string, extra ...string) *exec.Cmd {
	command := exec.Command(filepath.Join(fixture.root, "scripts/publish-latest.sh"))
	command.Env = append(os.Environ(),
		"PATH="+fixture.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GCS_ROOT="+fixture.storage,
		"FAKE_REQUESTS="+fixture.requests,
		"QODO_SUPPORT_BUNDLE_RELEASE_DIR="+fixture.release,
		"QODO_SUPPORT_BUNDLE_VERSION="+version,
	)
	command.Env = append(command.Env, extra...)
	return command
}

func (fixture latestFixture) object(name string) string {
	return filepath.Join(fixture.storage, "qodo-cli-public/support-bundle", name)
}

func writeLatestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
