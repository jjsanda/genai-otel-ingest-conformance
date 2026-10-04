package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Main(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestMainNoArgsPrintsUsage(t *testing.T) {
	code, _, stderr := runCLI(t)
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr does not contain usage text:\n%s", stderr)
	}
}

func TestMainVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, "version")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, Version) {
		t.Errorf("stdout %q does not contain version %q", stdout, Version)
	}
}

func TestMainHelp(t *testing.T) {
	code, stdout, _ := runCLI(t, "help")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, "Commands:") {
		t.Errorf("stdout does not contain command list:\n%s", stdout)
	}
}

func TestMainUnknownCommand(t *testing.T) {
	code, _, stderr := runCLI(t, "bogus")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, `unknown command "bogus"`) {
		t.Errorf("stderr does not mention unknown command:\n%s", stderr)
	}
}
