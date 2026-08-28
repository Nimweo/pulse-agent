package updater

import "testing"

func TestVersionComparison(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "0.9.0", right: "0.9.0", want: 0},
		{left: "v0.10.0", right: "0.9.9", want: 1},
		{left: "1.0.0", right: "0.99.99", want: 1},
		{left: "0.9.1", right: "0.10.0", want: -1},
	}
	for _, test := range tests {
		left, err := parseVersion(test.left)
		if err != nil {
			t.Fatalf("parseVersion(%q) error = %v", test.left, err)
		}
		right, err := parseVersion(test.right)
		if err != nil {
			t.Fatalf("parseVersion(%q) error = %v", test.right, err)
		}
		if got := left.Compare(right); got != test.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestParseVersionRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "1", "1.2", "1.2.3.4", "1.2.x", "v1.2.3-beta"} {
		if _, err := parseVersion(value); err == nil {
			t.Errorf("parseVersion(%q) error = nil", value)
		}
	}
}

func TestReleasePackage(t *testing.T) {
	packageName, archiveName, binaryName, err := releasePackage("0.10.0", "windows", "arm64")
	if err != nil {
		t.Fatalf("releasePackage() error = %v", err)
	}
	if packageName != "pulse-agent_0.10.0_windows_arm64" ||
		archiveName != "pulse-agent_0.10.0_windows_arm64.zip" ||
		binaryName != "pulse-agent.exe" {
		t.Fatalf("release package = %q/%q/%q", packageName, archiveName, binaryName)
	}
}
