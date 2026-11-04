package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxAssetSize GitHub release asset sınırı (2 GiB); biraz pay bırakıldı.
const maxAssetSize = 2000 << 20

// errRateLimited: GitHub API sınırına gelindi; bu çalıştırmada başka yükleme yapılmaz,
// kalanlar bir sonraki çalıştırmaya kalır (hata sayılmaz).
var errRateLimited = errors.New("GitHub API sınırına gelindi, release yüklemesi sonraki çalıştırmaya kaldı")

// releaser kaynak dosyaları GitHub Release'lerine yükler (gh CLI). Her yasama yılı
// (ya da toplantı yılı / içtima) bir release; asset adları dosyanın özgün adıdır.
//
// Actions'taki GITHUB_TOKEN repo başına saatte 1.000 REST isteği veriyor; `gh release
// upload` her çağrıda release'i sorguluyor. İstekler saatte perHour ile sınırlanır; sınır
// yine de aşılırsa yükleme bu çalıştırma için durur, kalanlar sonraki çalıştırmaya kalır.
type releaser struct {
	repo     string // owner/repo
	repoArgs []string
	mu       sync.Mutex
	tags     map[string]bool
	stopped  atomic.Bool

	lim     sync.Mutex
	tokens  float64
	perSec  float64
	lastRef time.Time
}

const releaseBurst = 40

func newReleaser(repo string, perHour int) (*releaser, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("-release için `gh` CLI gerekli")
	}
	rl := &releaser{tags: map[string]bool{}, tokens: releaseBurst, perSec: float64(max(1, perHour)) / 3600, lastRef: time.Now()}
	if repo == "" { // lokal: gh'nin varsayılan reposu
		out, err := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return nil, errors.New("release reposu bulunamadı; -repo owner/repo verin")
		}
		repo = strings.TrimSpace(string(out))
	}
	rl.repo, rl.repoArgs = repo, []string{"--repo", repo}
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

// Stopped API sınırına gelinip yüklemenin durdurulduğunu bildirir.
func (rl *releaser) Stopped() bool { return rl != nil && rl.stopped.Load() }

// take n istek hakkı gelene kadar bekler (token bucket).
func (rl *releaser) take(n int) {
	rl.lim.Lock()
	defer rl.lim.Unlock()
	for {
		now := time.Now()
		rl.tokens = min(releaseBurst, rl.tokens+now.Sub(rl.lastRef).Seconds()*rl.perSec)
		rl.lastRef = now
		if rl.tokens >= float64(n) {
			rl.tokens -= float64(n)
			return
		}
		time.Sleep(time.Duration((float64(n) - rl.tokens) / rl.perSec * float64(time.Second)))
	}
}

func (rl *releaser) gh(args ...string) (string, error) {
	cmd := exec.Command("gh", append(args, rl.repoArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "rate limit") {
			if !rl.stopped.Swap(true) {
				log.Printf("UYARI GitHub API sınırı: %s — bu çalıştırmada release yüklemesi durduruldu", msg)
			}
			return "", errRateLimited
		}
		return "", fmt.Errorf("gh %s %s: %v: %s", args[0], args[1], err, msg)
	}
	return string(out), nil
}

func (rl *releaser) upload(tag, title string, files []string) error {
	if rl.Stopped() {
		return errRateLimited
	}
	rl.mu.Lock()
	if !rl.tags[tag] {
		rl.take(2)
		_, err := rl.gh("release", "create", tag,
			"--title", title,
			"--notes", title+"."+string(rune(10))+string(rune(10))+"TBMM tutanaklarının özgün dosyaları (html/docx/pdf). "+
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
	// Ölçüm (2026-11-04): sınır 722 yüklemede doldu → dosya yüklemeleri (uploads.github.com)
	// bu sınıra sayılmıyor; sayılan, yükleme başına release sorgusu (~1-2 istek).
	rl.take(2)
	_, err := rl.gh(append([]string{"release", "upload", tag}, append(files, "--clobber")...)...)
	return err
}
