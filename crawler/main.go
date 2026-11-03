// crawler: TBMM tutanaklarını indirip data/ altında güncel tutar.
//
// Kaynaklar:
//   - tbmm.gov.tr (tbmm.go): yeni site, TBMM Genel Kurulu dönem 20'den bugüne.
//     Durum Ham -> Tam -> Basılmış değişince metin yeniden indirilir.
//   - www5 (www5.go): basılmış tutanak dergisi PDF arşivi, 1908'den bugüne tüm meclisler
//     ve gizli celseler. TBMM dönem 20+ için yalnızca yeni sitede olmayan birleşimler alınır.
//
// Metin önce html'den, olmazsa docx'ten, o da olmazsa pdf'ten çıkarılır ve
// data/<klasör>/YYYY-MM-DD.txt olarak yazılır (TBMM: data/tutanaklar/). Kaynak dosya
// sunucudan silinmişse (404) Wayback Machine'deki kopyası denenir. -release ile özgün
// dosyalar yasama yılı başına GitHub Release'lerine yüklenir. data/tutanaklar.json ve
// .csv birkaç saniyede bir yazılır; iş yarıda kesilirse sonraki çalıştırma kaldığı
// yerden devam eder.
//
// Repo kökünden çalıştırılır:
//
//	go run ./crawler                         yeni/değişen tutanakları indir
//	go run ./crawler -liste                  sadece ne yapılacağını göster
//	go run ./crawler -meclis MM -limit 5     deneme
//	go run ./crawler -release -sure-dk 300   CI'daki çalıştırma
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Korumalar
const (
	// Bir release grubundaki (yasama yılı) birleşim sayısı kayıtlı sayının bu oranından
	// azsa site sorunlu sayılır ve elde iyi olan veri ezilmez.
	minRowRatio = 0.8
	// İşlenen birleşimlerde izin verilen hata oranı; aşılırsa iş başarısız döner.
	maxFailureRatio = 0.05
	// Manifest en fazla bu aralıkla diske yazılır (ve iş bitince).
	manifestSaveEvery = 15 * time.Second
)

type job struct {
	row      Row
	rec      Record // kaydın kopyası; işçi bunu doldurup geri yollar
	wantText bool
}

type result struct {
	job
	err error
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		onlyList = flag.Bool("liste", false, "sadece listeleri tara, indirme yapma")
		limit    = flag.Int("limit", 0, "en fazla N birleşim işle (0 = sınırsız)")
		ornek    = flag.Int("ornek", 0, "deneme: her meclisten en fazla N birleşim işle")
		sureDk   = flag.Float64("sure-dk", 0, "N dakika sonra yeni işe başlama (0 = sınırsız)")
		release  = flag.Bool("release", false, "kaynak dosyaları GitHub Release'e yükle (gh CLI)")
		repo     = flag.String("repo", os.Getenv("GITHUB_REPOSITORY"), "release için owner/repo")
		workers  = flag.Int("paralel", 2, "aynı anda işlenen birleşim sayısı")
		kaynakF  = flag.String("kaynak", "tbmm,www5", "kullanılacak kaynaklar")
		meclisF  = flag.String("meclis", "", "sadece bu meclis(ler), ör. TBMM,MM,CS (boş = hepsi)")
		veri     = flag.String("veri", "data", "veri klasörü")
		cache    = flag.String("cache", envOr("CRAWLER_CACHE", "tutanak_cache"), "HTTP yanıtlarını bu klasörde sakla ve oradan oku (geliştirme için; -cache \"\" = kapalı)")
		donemler intList
	)
	flag.Var(&donemler, "donem", "sadece bu dönem(ler), ör. -donem 28 ya da -donem 27,28")
	flag.Parse()
	log.SetFlags(log.Ltime)
	setDataDir(*veri)
	cacheDir = *cache
	if cacheDir != "" {
		log.Printf("Yerel önbellek açık: %s", cacheDir)
	}
	started := time.Now()

	manifest, err := loadManifest()
	if err != nil {
		log.Print(err)
		return 1
	}
	kaynaklar := splitSet(*kaynakF, false)
	meclisSet := splitSet(*meclisF, true)

	// 1. Kaynakları tara. Bir kaynak okunamazsa diğeriyle devam edilir, iş sonunda
	// başarısız döner.
	srcFailed := false
	var rows []Row
	tbmmOK := false
	tbmmSite := map[[3]int]bool{}
	if kaynaklar["tbmm"] {
		r, err := collectTBMM(donemler)
		if err != nil {
			log.Printf("HATA tbmm.gov.tr okunamadı: %v", err)
			srcFailed = true
		} else {
			tbmmOK = true
			for _, x := range r {
				tbmmSite[[3]int{x.Donem, x.YasamaYili, x.Birlesim}] = true
			}
			rows = append(rows, r...)
		}
	}
	if kaynaklar["www5"] {
		r, err := collectWWW5(tbmmSite, tbmmOK, donemler)
		if err != nil {
			log.Printf("HATA www5 okunamadı: %v", err)
			srcFailed = true
		} else {
			rows = append(rows, r...)
		}
	}
	if len(meclisSet) > 0 {
		kept := rows[:0]
		for _, r := range rows {
			if meclisSet[r.Meclis] {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	if len(rows) == 0 {
		log.Print("Hiç birleşim bulunamadı.")
		return 1
	}

	// 2. Site sorunluysa (liste beklenenden çok kısa) veriyi ezme.
	known, fresh := map[string]int{}, map[string]int{}
	for _, r := range manifest {
		known[r.ReleaseEtiket]++
	}
	for _, r := range rows {
		fresh[r.Tag]++
	}
	for tag, n := range fresh {
		if k := known[tag]; k > 0 && float64(n) < float64(k)*minRowRatio {
			log.Printf("%s listesi beklenenden kısa (%d < %d); site sorunlu olabilir, veri ezilmedi.", tag, n, k)
			return 1
		}
	}

	// 3. Yapılacaklar
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.ID] = row
		rec, ok := manifest[row.ID]
		if !ok {
			rec = &Record{ID: row.ID}
			manifest[row.ID] = rec
		}
		row.apply(rec)
	}
	assignFilenames(manifest, byID)
	var jobs []job
	for _, row := range rows {
		rec := manifest[row.ID]
		wantText := rec.Durum != row.Durum ||
			(rec.MetinKaynagi != "yok" && (rec.Dosya == "" || !fileExists(dataPath(rec.Dosya))))
		wantRelease := *release && rec.ReleaseDurum != row.Durum
		if wantText || wantRelease {
			jobs = append(jobs, job{row: row, rec: *rec, wantText: wantText})
		}
	}
	// Önce metni eksik olanlar, sonra sadece release bekleyenler; her grupta en yeniler önce.
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].wantText != jobs[j].wantText {
			return jobs[i].wantText
		}
		return less(&jobs[j].rec, &jobs[i].rec)
	})
	summarizeJobs(rows, jobs)

	if *onlyList {
		for i, j := range jobs {
			if i == 50 {
				log.Printf("  … ve %d tane daha", len(jobs)-50)
				break
			}
			extra := ""
			if !j.wantText {
				extra = " (sadece release)"
			}
			log.Printf("  %s %s → %s%s", j.rec.label(), j.row.Durum, j.rec.Dosya, extra)
		}
		return boolExit(srcFailed)
	}
	if *ornek > 0 {
		per, kept := map[string]int{}, jobs[:0]
		for _, j := range jobs {
			if per[j.row.Meclis] < *ornek {
				per[j.row.Meclis]++
				kept = append(kept, j)
			}
		}
		jobs = kept
	}
	if *limit > 0 && len(jobs) > *limit {
		jobs = jobs[:*limit]
	}

	var rl *releaser
	if *release {
		if rl, err = newReleaser(*repo); err != nil {
			log.Print(err)
			return 1
		}
	}
	tmpDir, err := os.MkdirTemp("", "tutanak-")
	if err != nil {
		log.Print(err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	// 4. İşçiler. Kayıtları yalnızca bu goroutine günceller ve diske yazar.
	jobCh := make(chan job)
	resCh := make(chan result)
	var wg sync.WaitGroup
	for i := 0; i < max(1, *workers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				rec, err := process(j, rl, tmpDir)
				j.rec = rec
				resCh <- result{job: j, err: err}
			}
		}()
	}
	go func() {
		defer close(jobCh)
		for _, j := range jobs {
			if *sureDk > 0 && time.Since(started) > time.Duration(*sureDk*float64(time.Minute)) {
				log.Print("Süre sınırına gelindi; kalanlar sonraki çalıştırmaya.")
				return
			}
			jobCh <- j
		}
	}()
	go func() { wg.Wait(); close(resCh) }()

	done, failed := 0, 0
	lastSave := time.Now()
	for res := range resCh {
		label := res.rec.label() + " " + res.row.Durum
		if res.err != nil {
			failed++
			log.Printf("HATA %s: %v", label, res.err)
			// Linkler yine de kaydedilsin; durum değişmediği için sonraki çalıştırmada tekrar denenir.
			rec := manifest[res.rec.ID]
			rec.HTML, rec.DOCX, rec.PDF, rec.BasilmisPDF = res.rec.HTML, res.rec.DOCX, res.rec.PDF, res.rec.BasilmisPDF
		} else {
			done++
			*manifest[res.rec.ID] = res.rec
			log.Printf("OK   %s → %s (%s)", label, res.rec.Dosya, res.rec.MetinKaynagi)
		}
		// Manifest büyük; her birleşimde değil, birkaç saniyede bir yazılır. Kesintide
		// kaydedilmemiş birleşimler bir sonraki çalıştırmada yeniden indirilir.
		if time.Since(lastSave) > manifestSaveEvery {
			if err := saveManifest(manifest); err != nil {
				log.Printf("manifest yazılamadı: %v", err)
				return 1
			}
			lastSave = time.Now()
		}
	}
	if err := saveManifest(manifest); err != nil {
		log.Printf("manifest yazılamadı: %v", err)
		return 1
	}

	remaining := len(jobs) - done - failed
	summary := fmt.Sprintf("İşlenen: %d, hatalı: %d, kalan: %d, toplam kayıt: %d, süre: %s",
		done, failed, remaining, len(manifest), time.Since(started).Round(time.Second))
	log.Print(summary)
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "### Tutanak güncellemesi\n\n%s\n", summary)
			f.Close()
		}
	}
	if failed > 0 && float64(failed) > max(1, float64(done+failed)*maxFailureRatio) {
		log.Printf("Hata oranı çok yüksek (%d/%d).", failed, done+failed)
		return 1
	}
	return boolExit(srcFailed)
}

// process bir birleşimin metnini çıkarır ve (-release ise) kaynaklarını yükler.
func process(j job, rl *releaser, tmpDir string) (Record, error) {
	rec, row := j.rec, j.row

	// Kaynak dosyalar: tür -> (url, release'teki adı). Metin, textKinds sırasıyla denenir.
	var links map[string]string
	var textKinds, assetKinds []string
	assetName := func(kind string) string { return path.Base(links[kind]) }
	if row.Kaynak == kaynakTBMM {
		doc, err := fetchDoc(fmt.Sprintf(detailURLFmt, rec.ID))
		if err != nil {
			return rec, err
		}
		links = parseDetail(doc)
		rec.HTML, rec.DOCX, rec.PDF, rec.BasilmisPDF = links["html"], links["docx"], links["pdf"], links["basilmis_pdf"]
		if rec.HTML == "" && rec.DOCX == "" && rec.PDF == "" {
			return rec, fmt.Errorf("detay sayfasında html/docx/pdf linki yok")
		}
		// Basılmış PDF (20-40 MB) yüklenmez, linki manifestte.
		textKinds, assetKinds = []string{"html", "docx", "pdf"}, []string{"html", "docx", "pdf"}
		assetName = func(kind string) string {
			return fmt.Sprintf("d%d-y%d-b%03d.%s", rec.Donem, rec.YasamaYili, rec.Birlesim, kind)
		}
	} else {
		links = row.Links
		textKinds, assetKinds = []string{"pdf"}, []string{"pdf", "gundem_pdf", "eski_harfli_pdf"}
	}

	// Dosyalar bellekte değil iş klasöründe tutulur (1,7 GB'lık birleşim PDF'i var).
	dir, err := os.MkdirTemp(tmpDir, "j")
	if err != nil {
		return rec, err
	}
	defer os.RemoveAll(dir)
	got := map[string]string{}
	get := func(kind string) (string, error) {
		if p, ok := got[kind]; ok {
			return p, nil
		}
		p := filepath.Join(dir, assetName(kind))
		wb, err := download(links[kind], p)
		if err != nil {
			return "", err
		}
		got[kind] = p
		rec.Wayback = rec.Wayback || wb
		return p, nil
	}

	if j.wantText {
		var netErrs, empty []string
		source := ""
		for _, kind := range textKinds {
			if links[kind] == "" {
				continue
			}
			p, err := get(kind)
			if err != nil {
				netErrs = append(netErrs, fmt.Sprintf("%s: %v", kind, err))
				continue
			}
			text, err := extractFile(kind, p)
			text = normalize(text)
			if err != nil || strings.TrimSpace(text) == "" {
				empty = append(empty, fmt.Sprintf("%s: %v", kind, err))
				continue
			}
			if err := writeAtomic(dataPath(rec.Dosya), []byte(text)); err != nil {
				return rec, err
			}
			source = kind
			break
		}
		switch {
		case source != "":
			rec.MetinKaynagi = source
		case len(netErrs) > 0:
			return rec, fmt.Errorf("indirilemedi: %s", strings.Join(netErrs, "; "))
		default:
			// Dosya indi ama metin katmanı yok (taranmış görüntü) ya da okunamadı;
			// her gün yeniden denemenin anlamı yok.
			log.Printf("UYARI %s: metin yok (%s)", rec.label(), strings.Join(empty, "; "))
			rec.MetinKaynagi, rec.Dosya = "yok", ""
		}
	}

	if rl != nil {
		var files []string
		for _, kind := range assetKinds {
			if links[kind] == "" {
				continue
			}
			p, err := get(kind)
			if err != nil {
				return rec, err
			}
			if st, err := os.Stat(p); err == nil && st.Size() >= maxAssetSize {
				log.Printf("UYARI %s: %s %d MB, GitHub'ın dosya sınırını aştığı için release'e yüklenmedi (linki manifestte)",
					rec.label(), path.Base(p), st.Size()>>20)
				continue
			}
			files = append(files, p)
		}
		if len(files) > 0 {
			if err := rl.upload(row.Tag, row.TagTitle, files); err != nil {
				return rec, err
			}
		}
		rec.ReleaseDurum = row.Durum
	}
	rec.Durum = row.Durum
	return rec, nil
}

func summarizeJobs(rows []Row, jobs []job) {
	total, todo, text := map[string]int{}, map[string]int{}, map[string]int{}
	for _, r := range rows {
		total[r.Meclis]++
	}
	for _, j := range jobs {
		todo[j.row.Meclis]++
		if j.wantText {
			text[j.row.Meclis]++
		}
	}
	keys := make([]string, 0, len(total))
	for k := range total {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	nText := 0
	for _, k := range keys {
		log.Printf("  %-14s %5d birleşim, işlenecek %5d (metin %d)", k, total[k], todo[k], text[k])
		nText += text[k]
	}
	log.Printf("Toplam %d birleşim; işlenecek %d (metin: %d)", len(rows), len(jobs), nText)
}

// dataPath manifestteki "tutanaklar/2026-11-03.txt" yolunu diskteki yola çevirir.
func dataPath(rel string) string { return filepath.Join(dataDir, filepath.FromSlash(rel)) }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func boolExit(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

func splitSet(s string, upper bool) map[string]bool {
	set := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			if upper {
				p = strings.ToUpper(p)
			} else {
				p = strings.ToLower(p)
			}
			set[p] = true
		}
	}
	return set
}

type intList []int

func (l *intList) String() string { return fmt.Sprint(*l) }
func (l *intList) Set(s string) error {
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return err
		}
		*l = append(*l, n)
	}
	return nil
}
