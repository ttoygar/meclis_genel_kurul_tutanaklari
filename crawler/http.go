package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/charmap"
)

const userAgent = "Mozilla/5.0 (compatible; meclis-tutanak-guncelleyici; +https://github.com/ttoygar/meclis_genel_kurul_tutanaklari)"

// Ağ ayarları
const (
	maxAttempts    = 8
	baseBackoff    = 3 * time.Second // deneme başına ikiye katlanır
	maxBackoff     = 60 * time.Second
	rateLimitPause = 10 * time.Second // 403/429/503 sonrası ek bekleme
	// www5 bağlantı başına ~0.2-1 MB/s veriyor; 50 MB'lık PDF'ler dakikalar sürebiliyor.
	requestTimeout  = 20 * time.Minute
	headerTimeout   = 60 * time.Second        // sunucu hiç cevap vermiyorsa
	readIdleTimeout = 30 * time.Second        // indirme sırasında bu kadar süre veri gelmezse (sunucu bazen bağlantıyı açık tutup susuyor)
	maxDelay        = 1500 * time.Millisecond // her istekten önce rastgele bekleme üst sınırı
	minDelay        = 500 * time.Millisecond

	// Wayback Machine botlara karşı hassas; istekler arasında uzun bekle, az dene.
	waybackPrefix   = "https://web.archive.org/web/2id_/"
	waybackAttempts = 3
	waybackDelay    = 5 * time.Second
)

var (
	httpClient = &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: headerTimeout, MaxIdleConnsPerHost: 8},
	}

	// errNotFound: 404/410; tekrar denemenin anlamı yok.
	errNotFound = errors.New("bulunamadı")

	waybackMu sync.Mutex // Wayback'e aynı anda tek istek
)

func backoffFor(attempt int) time.Duration {
	wait := baseBackoff << (attempt - 1)
	if wait > maxBackoff {
		wait = maxBackoff
	}
	// Full jitter: aynı anda reddedilen işçiler siteye aynı anda dönmesin.
	return wait/2 + time.Duration(rand.Int63n(int64(wait/2)+1))
}

func fetch(url string) ([]byte, error) { return fetchN(url, maxAttempts, minDelay, maxDelay) }

func fetchN(url string, attempts int, dMin, dMax time.Duration) ([]byte, error) {
	if b, ok := cacheGet(url); ok {
		return b, nil
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			wait := backoffFor(attempt - 1)
			log.Printf("Tekrar denenecek (%d/%d, %s): %s — %v", attempt, attempts, wait.Round(time.Second), url, lastErr)
			time.Sleep(wait)
		}
		time.Sleep(dMin + time.Duration(rand.Int63n(int64(dMax-dMin)+1)))

		body, status, err := fetchOnce(url)
		switch {
		case err != nil:
			lastErr = err
		case status == 404 || status == 410:
			return nil, fmt.Errorf("%s: HTTP %d: %w", url, status, errNotFound)
		case status == 403 || status == 429 || status == 503:
			lastErr = fmt.Errorf("HTTP %d", status)
			time.Sleep(rateLimitPause)
		case status != 200:
			lastErr = fmt.Errorf("HTTP %d", status)
		case len(body) == 0:
			lastErr = errors.New("boş yanıt")
		default:
			cachePut(url, body)
			return body, nil
		}
	}
	return nil, fmt.Errorf("%d denemede alınamadı: %s: %w", attempts, url, lastErr)
}

// ------------------------------------------------------------------ Yerel önbellek

// cacheDir boş değilse (-cache) başarılı yanıtlar (HTTP 200, boş değil) burada saklanır
// ve aynı adres bir daha istenince siteye gidilmeden buradan verilir. Geliştirme
// içindir: liste sayfaları da saklandığından Ham -> Tam gibi değişiklikler görünmez.
// Hatalı yanıtlar saklanmadığı için tekrar denemeler her zaman siteye gider.
// Wayback yanıtları saklanmaz (engelleme sayfası da 200 dönebiliyor).
var cacheDir string

func cachePath(url string) string {
	sum := sha1.Sum([]byte(url))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(cacheDir, h[:2], h)
}

func cacheGet(url string) ([]byte, bool) {
	if cacheDir == "" || strings.HasPrefix(url, waybackPrefix) {
		return nil, false
	}
	b, err := os.ReadFile(cachePath(url))
	return b, err == nil && len(b) > 0
}

func cachePut(url string, body []byte) {
	if cacheDir == "" || strings.HasPrefix(url, waybackPrefix) {
		return
	}
	if err := writeAtomic(cachePath(url), body); err != nil {
		log.Printf("UYARI önbelleğe yazılamadı: %v", err)
	}
}

func fetchOnce(url string) ([]byte, int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	// www5 bazen bağlantıyı açık tutup veri göndermeyi bırakıyor; readIdleTimeout boyunca
	// tek bayt gelmezse isteği kes (tekrar denenir).
	timer := time.AfterFunc(readIdleTimeout, cancel)
	defer timer.Stop()
	body, err := io.ReadAll(&idleReader{r: resp.Body, t: timer})
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%s boyunca veri gelmedi: %w", readIdleTimeout, err)
	}
	return body, resp.StatusCode, err
}

// idleReader her okumada boşta kalma sayacını sıfırlar.
type idleReader struct {
	r io.Reader
	t *time.Timer
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.t.Reset(readIdleTimeout)
	}
	return n, err
}

// fetchDoc UTF-8 sayfaları (tbmm.gov.tr) okur.
func fetchDoc(url string) (*goquery.Document, error) {
	body, err := fetch(url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

// fetchDoc1254 windows-1254 / iso-8859-9 sayfaları (www5) okur.
func fetchDoc1254(url string) (*goquery.Document, error) {
	body, err := fetch(url)
	if err != nil {
		return nil, err
	}
	utf, err := charmap.Windows1254.NewDecoder().Bytes(body)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(bytes.NewReader(utf))
}

// download kaynak dosyayı dst'ye indirir. Dosya belleğe alınmaz (1,7 GB'lık birleşim
// PDF'i var). www5 kaldığı yerden devam etmeye izin vermiyor (Range içeren istekler ana
// sayfaya yönlendiriliyor), o yüzden kesilen indirme baştan alınır. Sunucu dosya yok
// derse (404/410) aynı adresin Wayback Machine'deki ham kopyası denenir.
func download(url, dst string) (fromWayback bool, err error) {
	if cacheCopy(url, dst) {
		return false, nil
	}
	err = downloadRetry(url, dst, maxAttempts, minDelay, maxDelay)
	if err == nil && !looksLikeFile(url, dst) {
		err = errors.New("indirilen dosya beklenen türde değil")
	}
	if err == nil {
		cacheStore(url, dst)
		return false, nil
	}
	if !errors.Is(err, errNotFound) {
		return false, err
	}
	waybackMu.Lock()
	defer waybackMu.Unlock()
	wbErr := downloadRetry(waybackPrefix+url, dst, waybackAttempts, waybackDelay, 2*waybackDelay)
	if wbErr == nil && !looksLikeFile(url, dst) {
		wbErr = errors.New("Wayback geçerli dosya döndürmedi (engelleme sayfası olabilir)")
	}
	if wbErr != nil {
		os.Remove(dst)
		return false, fmt.Errorf("%v; wayback: %v", err, wbErr)
	}
	log.Printf("Wayback'ten alındı: %s", url)
	return true, nil
}

func downloadRetry(url, dst string, attempts int, dMin, dMax time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			wait := backoffFor(attempt - 1)
			log.Printf("Tekrar denenecek (%d/%d, %s): %s — %v", attempt, attempts, wait.Round(time.Second), url, lastErr)
			time.Sleep(wait)
		}
		time.Sleep(dMin + time.Duration(rand.Int63n(int64(dMax-dMin)+1)))

		n, status, err := fetchToFile(url, dst)
		switch {
		case err == nil && status == 200 && n > 0:
			return nil
		case status == 404 || status == 410:
			return fmt.Errorf("%s: HTTP %d: %w", url, status, errNotFound)
		case status == 403 || status == 429 || status == 503:
			lastErr = fmt.Errorf("HTTP %d", status)
			time.Sleep(rateLimitPause)
		case err != nil:
			lastErr = err
		case status != 200:
			lastErr = fmt.Errorf("HTTP %d", status)
		default:
			lastErr = errors.New("boş yanıt")
		}
	}
	return fmt.Errorf("%d denemede alınamadı: %s: %w", attempts, url, lastErr)
}

// fetchToFile url'yi dst'ye yazar. Başka bir siteye yönlendirme (www5'in ana sayfaya
// attığı istekler gibi) hata sayılır; içerik eksik gelirse (Content-Length) hata döner.
func fetchToFile(url, dst string) (n int64, status int, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, resp.StatusCode, nil
	}
	if final, orig := resp.Request.URL.Host, req.URL.Host; final != orig && !strings.HasSuffix(final, "archive.org") {
		return 0, resp.StatusCode, fmt.Errorf("%s adresine yönlendirildi", resp.Request.URL)
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, resp.StatusCode, err
	}
	defer f.Close()
	timer := time.AfterFunc(readIdleTimeout, cancel)
	defer timer.Stop()
	n, err = io.Copy(f, &idleReader{r: resp.Body, t: timer})
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%s boyunca veri gelmedi (%d MB inmişti): %w", readIdleTimeout, n>>20, err)
	}
	if err == nil && resp.ContentLength > 0 && n != resp.ContentLength {
		err = fmt.Errorf("eksik indi (%d / %d bayt)", n, resp.ContentLength)
	}
	return n, resp.StatusCode, err
}

// looksLikeFile diskteki dosyanın başına bakıp uzantıyla uyuştuğunu doğrular.
func looksLikeFile(url, p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	return n > 0 && looksLike(url, head[:n])
}

// cacheCopy önbellekte varsa dosyayı dst'ye kopyalar.
func cacheCopy(url, dst string) bool {
	if cacheDir == "" || strings.HasPrefix(url, waybackPrefix) {
		return false
	}
	return copyFile(cachePath(url), dst) == nil
}

// cacheStore indirilen dosyayı önbelleğe kopyalar.
func cacheStore(url, src string) {
	if cacheDir == "" || strings.HasPrefix(url, waybackPrefix) {
		return
	}
	dst := cachePath(url)
	if err := copyFile(src, dst+".tmp"); err == nil {
		os.Rename(dst+".tmp", dst)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// looksLike indirilen içeriğin uzantıyla uyuştuğunu kabaca doğrular.
func looksLike(url string, b []byte) bool {
	u := strings.ToLower(url)
	switch {
	case strings.HasSuffix(u, ".pdf"):
		return bytes.HasPrefix(b, []byte("%PDF"))
	case strings.HasSuffix(u, ".docx"):
		return bytes.HasPrefix(b, []byte("PK"))
	case strings.HasSuffix(u, ".html"), strings.HasSuffix(u, ".htm"):
		return !bytes.Contains(b, []byte("abusive bot traffic")) && bytes.Contains(bytes.ToLower(b), []byte("<html"))
	}
	return true
}
