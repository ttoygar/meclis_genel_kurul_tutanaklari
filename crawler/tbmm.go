package main

// Kaynak 1: yeni TBMM sitesi (dönem 20'den bugüne). Her birleşimin detay sayfasında
// cdn.tbmm.gov.tr altındaki html/docx/pdf linkleri var; durum Ham -> Tam -> Basılmış değişir.

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL      = "https://www.tbmm.gov.tr"
	indexURL     = baseURL + "/Tutanaklar/TutanakMetinleri"
	listURLFmt   = baseURL + "/Tutanaklar/DoneminTutanakMetinleri?Donem=%d&YasamaYili=%d"
	detailURLFmt = baseURL + "/Tutanaklar/Tutanak?Id=%s"
)

var (
	aylar = map[string]time.Month{
		"ocak": 1, "şubat": 2, "mart": 3, "nisan": 4, "mayıs": 5, "haziran": 6,
		"temmuz": 7, "ağustos": 8, "eylül": 9, "ekim": 10, "kasım": 11, "aralık": 12,
	}

	reIndex  = regexp.MustCompile(`DoneminTutanakMetinleri\?Donem=(\d+)&(?:amp;)?YasamaYili=(\d+)`)
	reID     = regexp.MustCompile(`(?i)Id=([0-9a-f-]{36})`)
	reNumber = regexp.MustCompile(`\d+`)
	reTarih  = regexp.MustCompile(`(\d{1,2})\s+(\p{L}+)\s+(\d{4})`)
)

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }

// collectTBMM yeni sitedeki tüm birleşimleri listeler.
func collectTBMM(donemler []int) ([]Row, error) {
	doc, err := fetchDoc(indexURL)
	if err != nil {
		return nil, err
	}
	pairs := parseIndex(doc)
	if len(pairs) == 0 {
		return nil, fmt.Errorf("dönem/yasama yılı listesi boş geldi; site yapısı değişmiş olabilir")
	}
	var rows []Row
	for _, p := range pairs {
		if len(donemler) > 0 && !contains(donemler, p[0]) {
			continue
		}
		doc, err := fetchDoc(fmt.Sprintf(listURLFmt, p[0], p[1]))
		if err != nil {
			return nil, err
		}
		part := parseList(doc, p[0], p[1])
		log.Printf("tbmm.gov.tr d%d y%d: %d birleşim", p[0], p[1], len(part))
		rows = append(rows, part...)
	}
	return rows, nil
}

func parseIndex(doc *goquery.Document) [][2]int {
	seen := map[[2]int]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if m := reIndex.FindStringSubmatch(href); m != nil {
			d, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[2])
			seen[[2]int{d, y}] = true
		}
	})
	pairs := make([][2]int, 0, len(seen))
	for p := range seen {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	return pairs
}

// parseTarih "08 Ağustos 2023 Salı" biçimini YYYY-MM-DD'ye çevirir.
func parseTarih(s string) (string, bool) {
	m := reTarih.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	ay, ok := aylar[strings.ToLowerSpecial(unicode.TurkishCase, m[2])]
	if !ok {
		return "", false
	}
	gun, _ := strconv.Atoi(m[1])
	yil, _ := strconv.Atoi(m[3])
	return toDate(gun, int(ay), yil)
}

func tbmmTag(donem, yil int) (string, string) {
	return fmt.Sprintf("kaynak-tbmm-d%d-y%d", donem, yil),
		fmt.Sprintf("Kaynaklar: TBMM Genel Kurulu, Dönem %d, Yasama Yılı %d", donem, yil)
}

func parseList(doc *goquery.Document, donem, yil int) []Row {
	var rows []Row
	tag, title := tbmmTag(donem, yil)
	doc.Find("table tr").Each(func(_ int, tr *goquery.Selection) {
		a := tr.Find(`a[href*="Tutanak?Id="]`).First()
		if a.Length() == 0 {
			return
		}
		href, _ := a.Attr("href")
		var tds []string
		tr.Find("td").Each(func(_ int, td *goquery.Selection) { tds = append(tds, cleanText(td.Text())) })

		mID := reID.FindStringSubmatch(href)
		mB := reNumber.FindString(a.Text())
		var tarih string
		ok := len(tds) > 2
		if ok {
			tarih, ok = parseTarih(tds[1])
		}
		if mID == nil || mB == "" || !ok {
			log.Printf("UYARI satır çözümlenemedi (d%d y%d): %q", donem, yil, tds)
			return
		}
		b, _ := strconv.Atoi(mB)
		rows = append(rows, Row{
			ID: strings.ToLower(mID[1]), Kaynak: kaynakTBMM, Meclis: "TBMM",
			Donem: donem, YasamaYili: yil, Birlesim: b, Tarih: tarih, Durum: tds[2],
			Key: fmt.Sprintf("d%dy%db%03d", donem, yil, b), Dir: meclisler["TBMM"].Dir,
			Tag: tag, TagTitle: title,
		})
	})
	return rows
}

// parseDetail detay sayfasındaki cdn linklerini türüne göre ayırır.
func parseDetail(doc *goquery.Document) map[string]string {
	links := map[string]string{}
	doc.Find("a[href], embed[src], iframe[src], object[data]").Each(func(_ int, s *goquery.Selection) {
		url := s.AttrOr("href", s.AttrOr("src", s.AttrOr("data", "")))
		if !strings.Contains(url, "cdn.tbmm.gov.tr") {
			return
		}
		url = strings.SplitN(url, "#", 2)[0]
		p := strings.ToLower(url)
		if strings.Contains(p, "/ozet/") || strings.Contains(p, "/acikoylama") {
			return
		}
		ext := p[strings.LastIndex(p, ".")+1:]
		key := ext
		if ext == "pdf" && strings.Contains(p, "/basilmis/") {
			key = "basilmis_pdf"
		} else if ext != "html" && ext != "docx" && ext != "pdf" {
			return
		}
		if _, ok := links[key]; !ok {
			links[key] = url
		}
	})
	return links
}

// toDate geçerli bir takvim günü ise YYYY-MM-DD döndürür.
func toDate(gun, ay, yil int) (string, bool) {
	t := time.Date(yil, time.Month(ay), gun, 0, 0, 0, 0, time.UTC)
	if t.Day() != gun || int(t.Month()) != ay {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
