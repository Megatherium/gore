package xutil

import (
	"regexp"
	"strings"
)

var (
	reAmd64 = regexp.MustCompile(`(?i)[_.-](amd64|x86_64|x64)[_.-]`)
	reArm64 = regexp.MustCompile(`(?i)[_.-](arm64|aarch64)[_.-]`)
	reI386  = regexp.MustCompile(`(?i)[_.-](i386|i686|x86)[_.-]`)
	reArmhf = regexp.MustCompile(`(?i)[_.-](armhf|armv7)[_.-]`)
	reAll   = regexp.MustCompile(`(?i)[_.-]all[_.-]`)

	reRpmArch     = regexp.MustCompile(`\.([^.]+)\.rpm$`)
	reDebArchTail = regexp.MustCompile(`[_.-]([A-Za-z0-9]+)\.deb$`)
)

// ExtractArchFromFilename determines architecture from a Debian (.deb) or package filename.
// Defaults to "amd64" if no pattern matches.
func ExtractArchFromFilename(filename string) string {
	if reAmd64.MatchString(filename) {
		return "amd64"
	}
	if reArm64.MatchString(filename) {
		return "arm64"
	}
	// Avoid false positive on x86 when followed by 64 (e.g. x86_64 or x86-64)
	if loc := reI386.FindStringIndex(filename); loc != nil {
		rest := filename[loc[1]:]
		if !strings.HasPrefix(rest, "64") && !strings.HasPrefix(rest, "_64") && !strings.HasPrefix(rest, "-64") {
			return "i386"
		}
	}
	if reArmhf.MatchString(filename) {
		return "armhf"
	}
	if reAll.MatchString(filename) {
		return "all"
	}
	// Unknown but plausible architecture token (e.g. riscv64, ppc64le, s390x,
	// armel, mips64el): pass it through instead of silently mislabeling as amd64.
	// Purely numeric tokens are version fragments, not architectures.
	if m := reDebArchTail.FindStringSubmatch(filename); len(m) > 1 {
		token := strings.ToLower(m[1])
		if strings.ContainsAny(token, "abcdefghijklmnopqrstuvwxyz") {
			return token
		}
	}
	return "amd64"
}

// ExtractRpmArchFromFilename determines architecture from an RPM package filename.
// RPM filenames follow the pattern: name-version-release.arch.rpm
func ExtractRpmArchFromFilename(filename string) string {
	matches := reRpmArch.FindStringSubmatch(filename)
	if len(matches) > 1 {
		arch := matches[1]
		switch arch {
		case "x86_64", "amd64":
			return "x86_64"
		case "aarch64", "arm64":
			return "aarch64"
		case "i686", "i386":
			return "i686"
		case "noarch":
			return "noarch"
		default:
			return arch
		}
	}
	return "noarch"
}
