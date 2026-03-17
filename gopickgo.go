// The gopickgo command execs the "right" go or gofmt binaries
// based on your current directory. It looks up for a go.mod file
// and tries the ./tool/go relative to that, else $HOME/sdk/go/bin/go,
// else the highest semver $HOME/sdk/go*/bin/go, else /usr/local/go/bin/go,
// else /usr/local/bin/go, else /usr/bin/go.
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	path, err := findGo()
	if err != nil {
		if installErr := installGo(); installErr != nil {
			log.Fatalf("gopickgo: %v (install failed: %v)", err, installErr)
		}
		path, err = findGo()
		if err != nil {
			log.Fatalf("gopickgo: %v", err)
		}
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

func isInteractive() bool {
	for _, f := range []*os.File{os.Stdin, os.Stderr} {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		if fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

func installGo() error {
	if !isInteractive() {
		return errors.New("no Go found and stdin is not a terminal")
	}

	version, err := latestGoVersion()
	if err != nil {
		return fmt.Errorf("finding latest Go version: %w", err)
	}
	fmt.Fprintf(os.Stderr, "No Go found. Install %s? [y/N] ", version)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return errors.New("no input")
	}
	answer := strings.TrimSpace(scanner.Text())
	if answer != "y" && answer != "Y" {
		return errors.New("install declined")
	}

	url := fmt.Sprintf("https://go.dev/dl/%s.%s-%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(os.Stderr, "Downloading %s ...\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading Go: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("downloading Go: HTTP %s", resp.Status)
	}

	dest := filepath.Join(os.Getenv("HOME"), "sdk")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := extractTarGz(resp.Body, dest, version); err != nil {
		return fmt.Errorf("extracting to %s: %w", dest, err)
	}
	fmt.Fprintf(os.Stderr, "Installed to %s\n", filepath.Join(dest, version, "bin", "go"))
	return nil
}

func latestGoVersion() (string, error) {
	resp, err := http.Get("https://go.dev/dl/?mode=json")
	if err != nil {
		return "", fmt.Errorf("fetching go.dev/dl: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fetching go.dev/dl: HTTP %s", resp.Status)
	}
	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("parsing go.dev/dl response: %w", err)
	}
	for _, r := range releases {
		if r.Stable {
			return r.Version, nil
		}
	}
	return "", errors.New("no stable Go release found")
}

func extractTarGz(r io.Reader, destParent, dirRename string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		name := hdr.Name
		if dirRename != "" {
			// Rewrite "go/" prefix to "{dirRename}/"
			if strings.HasPrefix(name, "go/") {
				name = dirRename + name[2:]
			} else if name == "go" {
				name = dirRename
			}
		}

		target := filepath.Join(destParent, name)
		if !strings.HasPrefix(target, destParent+string(filepath.Separator)) && target != destParent {
			continue // skip entries that escape dest
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)|0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}
