// The gopickgo command execs the "right" go or gofmt binaries
// based on your current directory. It looks up for a go.mod file
// and tries the ./tool/go relative to that, else $HOME/sdk/go/bin/go,
// else the highest semver $HOME/sdk/go*/bin/go, else /usr/local/go/bin/go,
// else /usr/local/bin/go, else /usr/bin/go.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	path, err := findGo()
	if err != nil {
		log.Fatalf("gopickgo: %v", err)
	}
	wantGofmt := len(os.Args) >= 1 && filepath.Base(os.Args[0]) == "gofmt"
	if wantGofmt {
		toolDir, err := exec.Command(path, "env", "GOTOOLDIR").Output()
		if err != nil {
			log.Fatalf("gopickgo: error getting GOTOOLDIR: %v", err)
		}
		path = filepath.Join(strings.TrimSpace(string(toolDir)), "../../../bin/gofmt")
	}
	if len(os.Args) == 2 && os.Args[1] == "pick" {
		fmt.Println(path)
		return
	}
	err = syscall.Exec(path, os.Args, os.Environ())
	log.Fatalf("gopickgo: error execing %v: %v", path, err)
}

func findGo() (string, error) {
	self, _ := os.Executable()
	if self != "" {
		self, _ = filepath.EvalSymlinks(self)
	}

	var cands []string
	if mod, err := goModuleRoot(); err == nil {
		if strings.HasSuffix(mod, "/src") {
			cands = append(cands, filepath.Join(mod, "..", "bin", "go"))
		} else if strings.HasSuffix(mod, "/src/cmd") {
			cands = append(cands, filepath.Join(mod, "..", "..", "bin", "go"))
		} else {
			toolGo := filepath.Join(mod, "tool", "go")
			cands = append(cands, toolGo)
		}
	}
	cands = append(cands, filepath.Join(os.Getenv("HOME"), "sdk", "go", "bin", "go"))
	if best, err := bestSDKGo(); err == nil {
		cands = append(cands, best)
	}
	cands = append(cands,
		"/usr/local/go/bin/go",
		"/usr/local/bin/go",
		"/usr/bin/go",
	)
	for _, cand := range cands {
		if _, err := os.Stat(cand); err == nil {
			if self != "" {
				if resolved, err := filepath.EvalSymlinks(cand); err == nil && resolved == self {
					continue
				}
			}
			return cand, nil
		}
	}
	return "", fmt.Errorf("no go found in any of %q", cands)
}

// bestSDKGo globs ~/sdk/go* and returns the bin/go path for the
// highest semver version found.
func bestSDKGo() (string, error) {
	sdkDir := filepath.Join(os.Getenv("HOME"), "sdk")
	entries, err := filepath.Glob(filepath.Join(sdkDir, "go*"))
	if err != nil {
		return "", err
	}
	var bestPath string
	var bestVer semver
	for _, e := range entries {
		name := filepath.Base(e)
		ver, ok := parseSemver(strings.TrimPrefix(name, "go"))
		if !ok {
			continue
		}
		binGo := filepath.Join(e, "bin", "go")
		if _, err := os.Stat(binGo); err != nil {
			continue
		}
		if bestPath == "" || ver.compare(bestVer) > 0 {
			bestPath = binGo
			bestVer = ver
		}
	}
	if bestPath == "" {
		return "", errors.New("no go found in ~/sdk")
	}
	return bestPath, nil
}

type semver struct {
	major, minor, patch int
}

func parseSemver(s string) (semver, bool) {
	// Accept versions like "1.22", "1.22.1".
	// Walk the string looking for '.' separators to avoid allocating.
	var v semver
	var field int
	for {
		dot := strings.IndexByte(s, '.')
		var part string
		if dot < 0 {
			part = s
		} else {
			part = s[:dot]
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return semver{}, false
		}
		switch field {
		case 0:
			v.major = n
		case 1:
			v.minor = n
		case 2:
			v.patch = n
		default:
			return semver{}, false
		}
		field++
		if dot < 0 {
			break
		}
		s = s[dot+1:]
	}
	if field < 2 {
		return semver{}, false
	}
	return v, true
}

func intCmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (a semver) compare(b semver) int {
	if c := intCmp(a.major, b.major); c != 0 {
		return c
	}
	if c := intCmp(a.minor, b.minor); c != 0 {
		return c
	}
	return intCmp(a.patch, b.patch)
}

func goModuleRoot() (string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(pwd, "go.mod")); err == nil {
			return pwd, nil
		}
		if pwd == "/" {
			break
		}
		pwd = filepath.Dir(pwd)
	}
	return "", errors.New("no go.mod found")
}
