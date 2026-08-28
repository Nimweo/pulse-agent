package updater

import (
	"fmt"
	"strconv"
	"strings"
)

type version struct {
	major uint64
	minor uint64
	patch uint64
}

func parseVersion(value string) (version, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q must use MAJOR.MINOR.PATCH", value)
	}

	values := make([]uint64, 3)
	for index, part := range parts {
		if part == "" {
			return version{}, fmt.Errorf("version %q must use MAJOR.MINOR.PATCH", value)
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version{}, fmt.Errorf("version %q must use MAJOR.MINOR.PATCH", value)
		}
		values[index] = parsed
	}
	return version{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) Compare(other version) int {
	left := [...]uint64{v.major, v.minor, v.patch}
	right := [...]uint64{other.major, other.minor, other.patch}
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func releasePackage(releaseVersion string, goos string, goarch string) (
	string,
	string,
	string,
	error,
) {
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return "", "", "", fmt.Errorf("unsupported operating system: %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", "", "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	packageName := fmt.Sprintf("pulse-agent_%s_%s_%s", releaseVersion, goos, goarch)
	binaryName := "pulse-agent"
	archiveName := packageName + ".tar.gz"
	if goos == "windows" {
		binaryName += ".exe"
		archiveName = packageName + ".zip"
	}
	return packageName, archiveName, binaryName, nil
}
