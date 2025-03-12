// The gopickgo command execs the "right" go or gofmt binaries
// based on your current directory. It looks up for a go.mod file
// and tries the ./tool/go relative to that, else $HOME/sdk/go/bin/go,
// else /usr/local/go/bin/go, else /usr/local/bin/go, else /usr/bin/go.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	cands = append(cands,
		filepath.Join(os.Getenv("HOME"), "sdk", "go", "bin", "go"),
		"/usr/local/go/bin/go",
		"/usr/local/bin/go",
		"/usr/bin/go",
	)
	for _, cand := range cands {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no go found in any of %q", cands)
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
