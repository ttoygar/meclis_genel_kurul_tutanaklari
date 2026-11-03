package main

// Kaynak 2: TBMM Kütüphanesi'nin basılmış tutanak dergisi PDF arşivi (www5).
// 1908'den bugüne tüm meclisler: Meclis-i Mebusan/Ayan, TBMM, 1961-80 Millet Meclisi,
// Cumhuriyet Senatosu ve birleşik toplantılar, Kurucu/Temsilciler Meclisi, MBK, MGK,
// Danışma Meclisi ve gizli celseler. PDF'lerde OCR metin katmanı var.
//
// TBMM'nin dönem 20 ve sonrası yeni sitede olduğundan bu dönemlerden yalnızca yeni
// sitede bulunmayan birleşimler alınır (ör. dönem 20 / 1. yasama yılının ilk birleşimleri).

import (
	"fmt"
	"log"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

const (
	w5Base     = "https://www5.tbmm.gov.tr/develop/owa/"
	w5Index    = w5Base + "tutanak_dergisi_pdfler.meclis_donemleri?v_meclisdonem=0"
	w5IndexMMB = w5Base + "tutanak_dergisi_pdfler_mmb.meclis_donemleri?v_meclisdonem=0"

	// TBMM'de bu dönemden itibaren ana kaynak yeni site.
	tbmmSiteFirstDonem = 20
)

type meclisInfo struct{ Ad, Dir string }

// meclisler: manifestteki meclis kodu -> ad ve data/ altındaki klasör.
var meclisler = map[string]meclisInfo{
	"TBMM":          {"TBMM Genel Kurulu", "tutanaklar"},
	"TBMM-BIRLESIK": {"TBMM Birleşik Toplantısı (Millet Meclisi - Cumhuriyet Senatosu)", "meclisler/tbmm-birlesik-toplanti"},
	"MM":            {"Millet Meclisi", "meclisler/millet-meclisi"},
	"CS":            {"Cumhuriyet Senatosu", "meclisler/cumhuriyet-senatosu"},
	"KM":            {"Kurucu Meclis", "meclisler/kurucu-meclis"},
	"TM":            {"Temsilciler Meclisi", "meclisler/temsilciler-meclisi"},
	"MBK":           {"Milli Birlik Komitesi", "meclisler/milli-birlik-komitesi"},
	"DM":            {"Danışma Meclisi", "meclisler/danisma-meclisi"},
	"MGK":           {"Milli Güvenlik Konseyi", "meclisler/milli-guvenlik-konseyi"},
	"GZC":           {"TBMM Gizli Celse", "meclisler/tbmm-gizli-celse"},
	"MMB":           {"Meclis-i Mebusan", "meclisler/meclis-i-mebusan"},
	"MA":            {"Meclis-i Ayan", "meclisler/meclis-i-ayan"},
}

// www5 sayfasındaki meclis_kisa_adi / v_meclis -> meclis kodu
var w5Kodlar = map[string]string{
	"TBMM(MM-CS)": "TBMM-BIRLESIK", "CS": "CS", "MM": "MM", "KM": "KM", "TM": "TM",
	"MBK": "MBK", "DM": "DM", "MGK": "MGK", "mmb": "MMB", "ma": "MA",
}

var (
	reW5Num   = regexp.MustCompile(`\d+`)
	reNokta   = regexp.MustCompile(`(\d{1,2})\.+(\d{1,2})\.+(\d{4})`) // sitede "02.12..1325" gibi yazım hataları var
	reW5Cilt  = regexp.MustCompile(`(?i)c(?:ilt)?0*(\d+)$`)
	reW5Donem = regexp.MustCompile(`(?i)/[dt](\d+)/`)
)

// w5Page bir birleşim listesi sayfası ve o sayfadaki satırlara verilecek bilgiler.
type w5Page struct {
	url    string
	meclis string
	donem  int    // 0 ise PDF yolundaki d<NN>'den alınır
	yil    int    // 0 ise tarihten türetilir (deriveYil); yoksa sayfanın yılı
	grup   string // release etiketi son eki ("t05", "i01"); boşsa dönem/yıl'dan
	derive bool
}

// collectWWW5 arşivdeki tüm birleşimleri listeler. tbmmSite, yeni sitede bulunan
// TBMM birleşimleri (dönem, yıl, birleşim); bunlar www5'ten tekrar alınmaz.
func collectWWW5(tbmmSite map[[3]int]bool, tbmmOK bool, donemler []int) ([]Row, error) {
	pages, err := w5Pages()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var rows []Row
	for _, p := range pages {
		doc, err := fetchDoc1254(p.url)
		if err != nil {
			return nil, err
		}
		part := parseW5Page(doc, p)
		kept := part[:0]
		for _, r := range part {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if r.Meclis == "TBMM" && r.Donem >= tbmmSiteFirstDonem &&
				(!tbmmOK || tbmmSite[[3]int{r.Donem, r.YasamaYili, r.Birlesim}]) {
				continue // yeni sitede var (ya da yeni site okunamadı; çift kayıt olmasın)
			}
			if len(donemler) > 0 && !contains(donemler, r.Donem) {
				continue
			}
			kept = append(kept, r)
		}
		rows = append(rows, kept...)
	}
	fixYearTypos(rows)
	deriveYil(rows)
	for i := range rows {
		rows[i].Tag, rows[i].TagTitle = w5Tag(rows[i])
	}
	log.Printf("www5: %d sayfa, %d birleşim", len(pages), len(rows))
	return rows, nil
}

// w5Pages iki dizin sayfasından tüm birleşim listesi sayfalarını çıkarır.
func w5Pages() ([]w5Page, error) {
	var pages []w5Page
	add := func(p w5Page) { pages = append(pages, p) }

	doc, err := fetchDoc1254(w5Index)
	if err != nil {
		return nil, err
	}
	var yasamaYillari []string
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u, err := url.Parse(href)
		if err != nil {
			return
		}
		q := u.Query()
		switch {
		case strings.HasSuffix(u.Path, "tutanak_dergisi_pdfler.yasama_yillari"):
			yasamaYillari = append(yasamaYillari, w5Base+href)
		case strings.HasSuffix(u.Path, "tutanak_dergisi_pdfler.birlesimler_diger_meclisler"):
			kisa, d := q.Get("meclis_kisa_adi"), q.Get("diger_donem_adi")
			kod := w5Kodlar[kisa]
			if kod == "" {
				kod = strings.ToUpper(kisa)
				log.Printf("UYARI www5'te bilinmeyen meclis %q, %q koduyla alınıyor", kisa, kod)
			}
			n, _ := strconv.Atoi(strings.TrimLeft(d, "dt"))
			p := w5Page{url: w5Base + href, meclis: kod}
			if strings.HasPrefix(d, "t") { // toplantı yılı (CS, birleşik toplantı)
				p.yil, p.grup = n, fmt.Sprintf("t%02d", n)
			} else {
				p.donem, p.derive = n, true // d00: dönem PDF yolundan
			}
			add(p)
		case strings.HasSuffix(u.Path, "tutanak_dergisi_pdfler.birlesimler_gc"):
			n, _ := strconv.Atoi(q.Get("v_donem"))
			add(w5Page{url: w5Base + href, meclis: "GZC", donem: n, grup: fmt.Sprintf("d%02d", n)})
		}
	})
	if len(yasamaYillari) == 0 && len(pages) == 0 {
		return nil, fmt.Errorf("www5 dizini boş geldi; site yapısı değişmiş olabilir")
	}

	// TBMM: dönem -> yasama yılı sayfaları
	for _, yy := range yasamaYillari {
		doc, err := fetchDoc1254(yy)
		if err != nil {
			return nil, err
		}
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			u, err := url.Parse(href)
			if err != nil || !strings.HasSuffix(u.Path, "tutanak_dergisi_pdfler.birlesimler") {
				return
			}
			q := u.Query()
			d, _ := strconv.Atoi(strings.TrimLeft(q.Get("v_donem"), "d"))
			y, _ := strconv.Atoi(q.Get("v_yasama_yili"))
			add(w5Page{url: w5Base + href, meclis: "TBMM", donem: d, yil: y})
		})
	}

	// Meclis-i Mebusan / Ayan: dönem -> içtima sayfaları
	doc, err = fetchDoc1254(w5IndexMMB)
	if err != nil {
		return nil, err
	}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u, err := url.Parse(href)
		if err != nil || !strings.HasSuffix(u.Path, "birlesimler_diger_meclisler") {
			return
		}
		q := u.Query()
		kod := w5Kodlar[q.Get("v_meclis")]
		if kod == "" {
			return
		}
		d, _ := strconv.Atoi(q.Get("v_donem"))
		ic := q.Get("v_ictima") // "01", "f01" (fevkalade içtima)
		y, _ := strconv.Atoi(strings.TrimLeft(ic, "f"))
		if strings.HasPrefix(ic, "f") {
			y = 0
		}
		add(w5Page{url: w5Base + href, meclis: kod, donem: d, yil: y, grup: fmt.Sprintf("d%02d-i%s", d, ic)})
	})
	return pages, nil
}

// parseW5Page bir birleşim listesindeki satırları okur. Satırda ilk PDF birleşim
// tutanağı; "...gnd.pdf" gündem, "eht..." eski harfli (Osmanlıca) baskı; "...fih..."
// cilt fihristi (alınmaz). Tarih "23 Nisan 1920" ya da "04.12.1324" (Rumi) biçiminde.
func parseW5Page(doc *goquery.Document, p w5Page) []Row {
	var rows []Row
	doc.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if tr.Find("tr").Length() > 0 { // iç içe tablo kapsayıcısı
			return
		}
		links := map[string]string{}
		var mainText string
		tr.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href := w5URL(a.AttrOr("href", ""))
			base := strings.ToLower(path.Base(href))
			switch {
			case !strings.HasSuffix(base, ".pdf"), strings.Contains(base, "fih"):
				return
			case strings.HasSuffix(base, "gnd.pdf"):
				links["gundem_pdf"] = href
			case strings.HasPrefix(base, "eht"):
				links["eski_harfli_pdf"] = href
			case links["pdf"] == "":
				links["pdf"], mainText = href, a.Text()
			}
		})
		if links["pdf"] == "" {
			return
		}
		tarih, gun := "", ""
		tr.Find("td").Each(func(_ int, td *goquery.Selection) {
			t := cleanText(td.Text())
			if tarih == "" {
				tarih = parseAnyDate(t, p.meclis == "MMB" || p.meclis == "MA")
			}
			if _, ok := gunler[strings.ToLowerSpecial(unicode.TurkishCase, t)]; ok {
				gun = strings.ToLowerSpecial(unicode.TurkishCase, t)
			}
		})
		if tarih == "" {
			log.Printf("UYARI www5 satırında tarih yok: %s", links["pdf"])
			return
		}
		pdfURL := links["pdf"]
		rel := pdfURL[strings.Index(strings.ToUpper(pdfURL), "/TUTANAK/")+len("/TUTANAK/"):]
		dir, base := path.Split(rel)
		b, _ := strconv.Atoi(reW5Num.FindString(mainText))
		row := Row{
			ID:     strings.ToLower(strings.TrimSuffix(rel, path.Ext(rel))),
			Kaynak: kaynakWWW5, Meclis: p.meclis,
			Donem: p.donem, YasamaYili: p.yil, Birlesim: b,
			Tarih: tarih, Durum: "Basılmış",
			Key:   strings.ReplaceAll(strings.TrimSuffix(strings.ToLower(base), ".pdf"), "_", ""),
			Dir:   meclisDir(p.meclis),
			Links: links,
			grup:  p.grup,
			gun:   gun,
		}
		if m := reW5Cilt.FindStringSubmatch(strings.TrimSuffix(dir, "/")); m != nil {
			row.Cilt = m[1]
		}
		if row.Donem == 0 && p.grup == "" {
			if m := reW5Donem.FindStringSubmatch("/" + rel); m != nil {
				row.Donem, _ = strconv.Atoi(m[1])
			}
		}
		if p.derive {
			row.YasamaYili = -1 // deriveYil doldurur
		}
		rows = append(rows, row)
	})
	return rows
}

func meclisDir(kod string) string {
	if m, ok := meclisler[kod]; ok {
		return m.Dir
	}
	return "meclisler/" + strings.ToLower(kod)
}

// w5URL linki https://www5... biçimine getirir (eski www.tbmm.gov.tr/tutanaklar adresleri
// zaten oraya yönleniyor).
func w5URL(href string) string {
	href = strings.TrimSpace(href)
	href = strings.Replace(href, "http://www.tbmm.gov.tr/tutanaklar/", "https://www5.tbmm.gov.tr/tutanaklar/", 1)
	href = strings.Replace(href, "http://www5.tbmm.gov.tr/", "https://www5.tbmm.gov.tr/", 1)
	return href
}

// parseAnyDate "23 Nisan 1920" ya da "04.12.1324" biçimini okur. rumi ise (Mebusan,
// Ayan) tarih Rumi takvim kabul edilip miladiye çevrilir. Diğer meclislerde 1500'den
// küçük yıl yazım hatasıdır ("0190"); olduğu gibi döner, fixYearTypos düzeltir.
func parseAnyDate(s string, rumi bool) string {
	var gun, ay, y int
	if t, ok := parseTarih(s); ok {
		y, _ = strconv.Atoi(t[:4])
		ay, _ = strconv.Atoi(t[5:7])
		gun, _ = strconv.Atoi(t[8:10])
	} else if m := reNokta.FindStringSubmatch(s); m != nil {
		gun, _ = strconv.Atoi(m[1])
		ay, _ = strconv.Atoi(m[2])
		y, _ = strconv.Atoi(m[3])
	} else {
		return ""
	}
	if rumi && y < 1500 {
		return rumiToMiladi(gun, ay, y)
	}
	t, _ := toDate(gun, ay, y)
	return t
}

// rumiToMiladi Rumi (mali) takvim tarihini miladiye çevirir.
//   - 1334 ve sonrası (1918+): gün/ay miladi ile aynı, yıl +584.
//   - 1333: 1 Mart 1333 = 1 Mart 1917'den itibaren gün/ay miladi ile aynı.
//   - 1332 ve öncesi: Jülyen takvimi, yıl Mart'ta başlar (Ocak-Şubat bir sonraki
//     miladi yıla düşer); Jülyen -> Gregoryen farkı 1900 öncesi 12, sonrası 13 gün.
func rumiToMiladi(gun, ay, yil int) string {
	gy := yil + 584
	if yil <= 1332 && ay < 3 {
		gy++
	}
	t := time.Date(gy, time.Month(ay), gun, 0, 0, 0, 0, time.UTC)
	if int(t.Month()) != ay {
		return ""
	}
	if yil <= 1332 {
		fark := 13
		if t.Before(time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)) {
			fark = 12
		}
		t = t.AddDate(0, 0, fark)
	}
	return t.Format("2006-01-02")
}

// deriveYil, sayfası yasama yılı vermeyen meclislerde (MM, DM, MGK, KM, TM, MBK)
// yasama yılını tarihten türetir: dönemin ilk birleşiminden sonra her 1 Kasım'da
// (1981'den itibaren 1 Ekim'de) yeni yasama yılı başlar.
func deriveYil(rows []Row) {
	first := map[[2]string]string{}
	for _, r := range rows {
		if r.YasamaYili != -1 {
			continue
		}
		k := [2]string{r.Meclis, strconv.Itoa(r.Donem)}
		if f, ok := first[k]; !ok || r.Tarih < f {
			first[k] = r.Tarih
		}
	}
	for i, r := range rows {
		if r.YasamaYili != -1 {
			continue
		}
		f, _ := time.Parse("2006-01-02", first[[2]string{r.Meclis, strconv.Itoa(r.Donem)}])
		t, _ := time.Parse("2006-01-02", r.Tarih)
		yy := 1
		for y := f.Year(); y <= t.Year(); y++ {
			ay := time.November
			if y >= 1981 {
				ay = time.October
			}
			b := time.Date(y, ay, 1, 0, 0, 0, 0, time.UTC)
			if b.After(f) && !b.After(t) {
				yy++
			}
		}
		rows[i].YasamaYili = yy
	}
}

// w5Tag release etiketi ve başlığı.
func w5Tag(r Row) (string, string) {
	grup := r.grup
	ad := meclisler[r.Meclis].Ad
	if ad == "" {
		ad = r.Meclis
	}
	kod := strings.ToLower(r.Meclis)
	switch {
	case r.Meclis == "TBMM":
		return tbmmTag(r.Donem, r.YasamaYili)
	case strings.HasPrefix(grup, "t"):
		return fmt.Sprintf("kaynak-%s-%s", kod, grup),
			fmt.Sprintf("Kaynaklar: %s, Toplantı Yılı %d", ad, r.YasamaYili)
	case r.Meclis == "GZC":
		return fmt.Sprintf("kaynak-gzc-%s", grup), fmt.Sprintf("Kaynaklar: %s, Dönem %d", ad, r.Donem)
	case grup != "": // Mebusan / Ayan: dönem + içtima
		ic := strings.TrimPrefix(grup[strings.Index(grup, "-i")+2:], "0")
		if strings.HasPrefix(ic, "f") {
			ic = "Fevkalade " + strings.TrimLeft(ic[1:], "0")
		}
		return fmt.Sprintf("kaynak-%s-%s", kod, grup),
			fmt.Sprintf("Kaynaklar: %s, Dönem %d, %s. İçtima", ad, r.Donem, ic)
	}
	return fmt.Sprintf("kaynak-%s-d%02d-y%d", kod, r.Donem, r.YasamaYili),
		fmt.Sprintf("Kaynaklar: %s, Dönem %d, Yasama Yılı %d", ad, r.Donem, r.YasamaYili)
}

var gunler = map[string]time.Weekday{
	"pazar": time.Sunday, "pazartesi": time.Monday, "salı": time.Tuesday, "çarşamba": time.Wednesday,
	"perşembe": time.Thursday, "cuma": time.Friday, "cumartesi": time.Saturday,
}

// fixYearTypos sitedeki yıl yazım hatalarını düzeltir (ör. MGK 155. birleşim "26 Temmuz
// 2010", doğrusu 1982). Bir birleşimin yılı aynı cildin ortanca yılından 3 yıldan fazla
// sapıyorsa yıl, sayfadaki gün adının tuttuğu ortanca (±1) yıla; hiçbiri tutmuyorsa
// ortanca yıla çekilir (sitede gün adının da yanlış yazıldığı satırlar var).
func fixYearTypos(rows []Row) {
	groups := map[string][]int{}
	for i, r := range rows {
		k := r.Meclis + "|" + strconv.Itoa(r.Donem) + "|" + r.Cilt + "|" + r.grup
		groups[k] = append(groups[k], i)
	}
	for _, idx := range groups {
		if len(idx) < 3 {
			continue
		}
		years := make([]int, 0, len(idx))
		for _, i := range idx {
			y, _ := strconv.Atoi(rows[i].Tarih[:4])
			years = append(years, y)
		}
		sort.Ints(years)
		med := years[len(years)/2]
		for _, i := range idx {
			r := &rows[i]
			y, _ := strconv.Atoi(r.Tarih[:4])
			if y-med <= 3 && med-y <= 3 {
				continue
			}
			fixed, note := "", ""
			for _, cand := range []int{med, med - 1, med + 1} {
				t, err := time.Parse("2006-01-02", fmt.Sprintf("%04d", cand)+r.Tarih[4:])
				if err == nil && r.gun != "" && t.Weekday() == gunler[r.gun] {
					fixed = t.Format("2006-01-02")
					break
				}
			}
			if fixed == "" { // gün adı da hatalı olabiliyor; yılı ortancaya çek
				if t, err := time.Parse("2006-01-02", fmt.Sprintf("%04d", med)+r.Tarih[4:]); err == nil {
					fixed, note = t.Format("2006-01-02"), " (gün adı tutmadı)"
				} else {
					log.Printf("UYARI www5 tarihi şüpheli, düzeltilemedi: %s %s (cilt ortancası %d)", r.ID, r.Tarih, med)
					continue
				}
			}
			log.Printf("UYARI www5 tarih yazım hatası düzeltildi: %s %s -> %s%s", r.ID, r.Tarih, fixed, note)
			r.Tarih = fixed
		}
	}
}
