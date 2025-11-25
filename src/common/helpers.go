package common

import (
	"fmt"
	"runtime"
	"strings"
)

func NormalizeVersion(v string) string {
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "go") {
		return "go" + v
	}
	return v
}

func GetOS() string {
	return runtime.GOOS
}

func GetArch() string {
	return runtime.GOARCH
}

func FindBuild(all []GoRelease, ver, goos, arch string) (GoRelease, string, string, error) {
	for _, r := range all {
		if r.Version != ver {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == goos && f.Arch == arch {
				return r, f.Filename, f.Sha256, nil
			}
		}
		return r, "", "", fmt.Errorf("version %s exists but no archive for %s/%s", ver, goos, arch)
	}
	return GoRelease{}, "", "", fmt.Errorf("version %s not found", ver)
}
