package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

var dataDir, jsonFile, csvFile string

func setDataDir(d string) {
	dataDir = d
	jsonFile = filepath.Join(d, "tutanaklar.json")
	csvFile = filepath.Join(d, "tutanaklar.csv")
}

// Kaynak adları
const (
	kaynakTBMM = "tbmm.gov.tr" // https://www.tbmm.gov.tr/Tutanaklar/TutanakMetinleri
	kaynakWWW5 = "www5"        // https://www5.tbmm.gov.tr/develop/owa/tutanak_dergisi_pdfler...
)

// Record, data/tutanaklar.json ve .csv'deki bir birleşim kaydıdır.
type Record struct {
	ID            string `json:"id"`
	Meclis        string `json:"meclis"` // TBMM, MM, CS, GZC, MMB ... (bkz. meclisler)
	Kaynak        string `json:"kaynak"` // tbmm.gov.tr / www5
	Donem         int    `json:"donem"`
	YasamaYili    int    `json:"yasama_yili"` // CS ve birleşik toplantıda toplantı yılı, Mebusan/Ayan'da içtima
	Birlesim      int    `json:"birlesim"`
	Cilt          string `json:"cilt"`
	Tarih         string `json:"tarih"`         // YYYY-MM-DD (Rumi tarihler miladiye çevrilir)
	Durum         string `json:"durum"`         // indirilen metnin durumu: Ham / Tam / Basılmış
	Dosya         string `json:"dosya"`         // data/ altındaki txt yolu
	MetinKaynagi  string `json:"metin_kaynagi"` // html / docx / pdf; metin katmanı yoksa "yok"
	HTML          string `json:"html"`
	DOCX          string `json:"docx"`
	PDF           string `json:"pdf"`
	BasilmisPDF   string `json:"basilmis_pdf"`
	GundemPDF     string `json:"gundem_pdf"`
	EskiHarfliPDF string `json:"eski_harfli_pdf"` // Osmanlıca (görüntü) PDF, sadece 1920-28
	Wayback       bool   `json:"wayback"`         // en az bir dosya web.archive.org'dan alındı
	ReleaseEtiket string `json:"release_etiket"`
	ReleaseDurum  string `json:"release_durum"` // release'e yüklenen kaynakların durumu
}

var csvHeader = []string{
	"id", "meclis", "kaynak", "donem", "yasama_yili", "birlesim", "cilt", "tarih", "durum", "dosya",
	"metin_kaynagi", "html", "docx", "pdf", "basilmis_pdf", "gundem_pdf", "eski_harfli_pdf",
	"wayback", "release_etiket", "release_durum",
}

func (r Record) csvRow() []string {
	return []string{
		r.ID, r.Meclis, r.Kaynak, strconv.Itoa(r.Donem), strconv.Itoa(r.YasamaYili), strconv.Itoa(r.Birlesim),
		r.Cilt, r.Tarih, r.Durum, r.Dosya, r.MetinKaynagi, r.HTML, r.DOCX, r.PDF, r.BasilmisPDF,
		r.GundemPDF, r.EskiHarfliPDF, strconv.FormatBool(r.Wayback), r.ReleaseEtiket, r.ReleaseDurum,
	}
}

func (r Record) label() string {
	return fmt.Sprintf("%s %s d%d y%d b%03d", r.Tarih, r.Meclis, r.Donem, r.YasamaYili, r.Birlesim)
}

// less: tarih, meclis, dönem, yasama yılı, birleşim sırası (eskiden yeniye).
func less(a, b *Record) bool {
	switch {
	case a.Tarih != b.Tarih:
		return a.Tarih < b.Tarih
	case a.Meclis != b.Meclis:
		return a.Meclis < b.Meclis
	case a.Donem != b.Donem:
		return a.Donem < b.Donem
	case a.YasamaYili != b.YasamaYili:
		return a.YasamaYili < b.YasamaYili
	case a.Birlesim != b.Birlesim:
		return a.Birlesim < b.Birlesim
	}
	return a.ID < b.ID
}

// Row, bir kaynağın listesindeki birleşimdir (sitenin şu anki hali).
type Row struct {
	ID                          string
	Kaynak, Meclis              string
	Donem, YasamaYili, Birlesim int
	Cilt                        string
	Tarih, Durum                string
	Key                         string            // aynı gün çakışmasında dosya adı eki
	Dir                         string            // data/ altındaki klasör
	Tag, TagTitle               string            // release etiketi ve başlığı
	Links                       map[string]string // www5: pdf / gundem_pdf / eski_harfli_pdf
	grup                        string            // www5: release etiketi son eki ("t05", "d03-i01")
	gun                         string            // www5: sayfadaki gün adı (tarih doğrulaması için)
}

func (row Row) apply(rec *Record) {
	rec.Meclis, rec.Kaynak = row.Meclis, row.Kaynak
	rec.Donem, rec.YasamaYili, rec.Birlesim = row.Donem, row.YasamaYili, row.Birlesim
	rec.Cilt, rec.Tarih, rec.ReleaseEtiket = row.Cilt, row.Tarih, row.Tag
	if row.Links != nil {
		rec.PDF, rec.GundemPDF, rec.EskiHarfliPDF = row.Links["pdf"], row.Links["gundem_pdf"], row.Links["eski_harfli_pdf"]
	}
}

// --------------------------------------------------------------------- Manifest

func loadManifest() (map[string]*Record, error) {
	m := map[string]*Record{}
	b, err := os.ReadFile(jsonFile)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []*Record
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("%s okunamadı: %w", jsonFile, err)
	}
	for _, r := range recs {
		m[r.ID] = r
	}
	return m, nil
}

func sortedRecords(m map[string]*Record) []*Record {
	recs := make([]*Record, 0, len(m))
	for _, r := range m {
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return less(recs[j], recs[i]) }) // en yeni önce
	return recs
}

func writeAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// Windows'ta hedef dosya o an başka süreçte (editör, antivirüs) açıksa rename
	// "erişim engellendi" verebiliyor; kısa aralıklarla tekrar dene.
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmp, p); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

func saveManifest(m map[string]*Record) error {
	recs := sortedRecords(m)

	var jb bytes.Buffer
	enc := json.NewEncoder(&jb)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(recs); err != nil {
		return err
	}
	if err := writeAtomic(jsonFile, jb.Bytes()); err != nil {
		return err
	}

	var cb bytes.Buffer
	w := csv.NewWriter(&cb)
	_ = w.Write(csvHeader)
	for _, r := range recs {
		_ = w.Write(r.csvRow())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return writeAtomic(csvFile, cb.Bytes())
}

// assignFilenames dosya adı olmayan kayıtlara ad verir. Klasördeki bir tarihin ilk
// birleşimi YYYY-MM-DD.txt, aynı günün diğerleri YYYY-MM-DD_<key>.txt olur
// (ör. 2026-11-03_d28y5b012.txt). Atanan ad bir daha değişmez.
func assignFilenames(m map[string]*Record, keys map[string]Row) {
	taken := map[string]bool{}
	for _, r := range m {
		if r.Dosya != "" {
			taken[r.Dosya] = true
		}
	}
	recs := sortedRecords(m)
	for i := len(recs) - 1; i >= 0; i-- { // eskiden yeniye
		r := recs[i]
		row, ok := keys[r.ID]
		if r.Dosya != "" || r.MetinKaynagi == "yok" || !ok {
			continue
		}
		name := path.Join(row.Dir, r.Tarih+".txt")
		if taken[name] {
			name = path.Join(row.Dir, r.Tarih+"_"+row.Key+".txt")
		}
		r.Dosya = name
		taken[name] = true
	}
}
