package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// maxAssetSize GitHub release asset sınırı (2 GiB); biraz pay bırakıldı.
const maxAssetSize = 2000 << 20

// releaser kaynak dosyaları GitHub Release'lerine yükler (gh CLI). Her yasama yılı
// (ya da toplantı yılı / içtima) bir release; asset adları dosyanın özgün adıdır.
type releaser struct {
	repoArgs []string
	mu       sync.Mutex
	tags     map[string]bool
}

func newReleaser(repo string) (*releaser, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("-release için `gh` CLI gerekli")
	}
	rl := &releaser{tags: map[string]bool{}}
	if repo != "" {
		rl.repoArgs = []string{"--repo", repo}
	}
	out, err := rl.gh("release", "list", "--limit", "5000", "--json", "tagName")
	if err != nil {
		return nil, err
	}
	var items []struct {
		TagName string `json:"tagName"`
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil && strings.TrimSpace(out) != "" {
		return nil, err
	}
	for _, it := range items {
		rl.tags[it.TagName] = true
	}
	return rl, nil
}

func (rl *releaser) gh(args ...string) (string, error) {
	cmd := exec.Command("gh", append(args, rl.repoArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh %s %s: %v: %s", args[0], args[1], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (rl *releaser) upload(tag, title string, files []string) error {
	rl.mu.Lock()
	if !rl.tags[tag] {
		_, err := rl.gh("release", "create", tag,
			"--title", title,
			"--notes", title+".\n\nTBMM tutanaklarının özgün dosyaları (html/docx/pdf). "+
				"Hangi dosyanın hangi birleşime ait olduğu data/tutanaklar.csv içinde. "+
				"Kaynak: tbmm.gov.tr / www5.tbmm.gov.tr. Otomatik güncellenir.",
			"--latest=false")
		if err != nil {
			rl.mu.Unlock()
			return err
		}
		rl.tags[tag] = true
	}
	rl.mu.Unlock()
	_, err := rl.gh(append([]string{"release", "upload", tag}, append(files, "--clobber")...)...)
	return err
}
