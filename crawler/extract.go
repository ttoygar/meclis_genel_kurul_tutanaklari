package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var reBlankLines = regexp.MustCompile(`\n{4,}`)

// Sonrasına satır sonu konan HTML elemanları. Yeni (Aspose) tutanaklarda <p>'ler
// arasında satır sonu olmadığından bunlar olmadan metin bitişik çıkar.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// textFromHTML, BeautifulSoup get_text() gibi tüm metin düğümlerini birleştirir
// (head/style/script hariç), blok elemanlardan sonra satır sonu ekler.
// Karakter kümesi sayfanın meta etiketinden okunur (windows-1254 / windows-1252 olabiliyor).
func textFromHTML(data []byte) (string, error) {
	r, err := charset.NewReader(bytes.NewReader(data), "text/html")
	if err != nil {
		return "", err
	}
	root, err := html.Parse(r)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	newline := func() {
		if s := sb.String(); s != "" && !strings.HasSuffix(s, "\n") {
			sb.WriteByte('\n')
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "head" || n.Data == "style" || n.Data == "script") {
			return
		}
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockTags[n.Data] {
			newline()
		}
	}
	walk(root)
	return sb.String(), nil
}

func textFromDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return docxText(rc)
	}
	return "", errors.New("word/document.xml yok")
}

// textFromPDFFile metni pdftotext (Poppler) ile çıkarır. Go kütüphanesi kelime arasındaki
// boşlukları harf konumlarından hesaplamadığı için dijital PDF'lerde (ör. 2011 gizli
// celse) metni boşluksuz veriyor; pdftotext yoksa ya da hata verirse yine de ona düşülür.
func textFromPDFFile(p string) (string, error) {
	if pdftotextPath() != "" {
		text, err := pdftotext(p)
		if err == nil {
			return text, nil
		}
		log.Printf("UYARI pdftotext hata verdi, Go kütüphanesi deneniyor: %v", err)
	}
	if st, err := os.Stat(p); err == nil && st.Size() > maxGoPDFSize {
		return "", fmt.Errorf("pdftotext yok ve dosya Go kütüphanesi için çok büyük (%d MB)", st.Size()>>20)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return textFromPDFGo(data)
}

var (
	pdftotextOnce sync.Once
	pdftotextBin  string
)

func pdftotextPath() string {
	pdftotextOnce.Do(func() {
		pdftotextBin, _ = exec.LookPath("pdftotext")
		if pdftotextBin == "" {
			log.Print("UYARI pdftotext bulunamadı (poppler-utils); PDF metinlerinde kelime arası boşluklar eksik çıkabilir")
		}
	})
	return pdftotextBin
}

func pdftotext(p string) (string, error) {
	var out, stderr bytes.Buffer
	cmd := exec.Command(pdftotextPath(), "-enc", "UTF-8", "-nopgbrk", p, "-")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func textFromPDFGo(data []byte) (text string, err error) {
	defer func() { // kütüphane bozuk pdf'lerde panic atabiliyor
		if r := recover(); r != nil {
			err = fmt.Errorf("pdf okunamadı: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	tr, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	b, err := io.ReadAll(tr)
	return string(b), err
}

// extractFile diskteki kaynak dosyadan metni çıkarır.
func extractFile(kind, p string) (string, error) {
	if kind == "pdf" {
		return textFromPDFFile(p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	switch kind {
	case "html":
		return textFromHTML(data)
	case "docx":
		return textFromDOCX(data)
	}
	return "", fmt.Errorf("bilinmeyen tür %q", kind)
}

// Go PDF kütüphanesine (yedek yol) verilecek en büyük dosya; dosyayı belleğe alıyor.
const maxGoPDFSize = 300 << 20

// Eski PDF'lerin metin katmanında windows-1252 C1 bölgesinden kalan karakterler.
var c1Replacer = strings.NewReplacer(
	"\u0085", "…", "\u0091", "‘", "\u0092", "’", "\u0093", "“", "\u0094", "”",
	"\u0095", "•", "\u0096", "–", "\u0097", "—",
	"\u00ad\n", "", "\u00ad", "", // yumuşak tire: satır sonunda bölünmüş kelimeyi birleştir
)

// normalize: NBSP'yi boşluk yapar (kelime ayırıcı olarak da kullanılıyor), C1/yumuşak
// tire artıklarını düzeltir, satır sonlarını LF'ye çevirir, satır sonundaki boşlukları
// kırpar, art arda 2'den fazla boş satırı 2'ye indirir.
func normalize(s string) string {
	s = strings.ReplaceAll(s, string(rune(0xA0)), " ")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	s = c1Replacer.Replace(strings.Join(lines, "\n"))
	s = reBlankLines.ReplaceAllString(s, "\n\n\n")
	return strings.TrimSpace(s) + "\n"
}

// docxText word/document.xml'deki paragrafları satır satır birleştirir.
func docxText(r io.Reader) (string, error) {
	var sb strings.Builder
	dec := xml.NewDecoder(r)
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return sb.String(), nil
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				sb.WriteByte('\t')
			case "br", "cr":
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
}
