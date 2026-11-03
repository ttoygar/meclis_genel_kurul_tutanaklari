# TBMM Genel Kurul Tutanakları

1908'deki Meclis-i Mebusan'dan bugünkü TBMM'ye kadar meclis genel kurul tutanaklarının düz metin (`.txt`) arşivi. NLP ve görselleştirme çalışmalarında kullanılmak üzere hazırlandı. Veri GitHub Actions ile her gün 03:30 UTC'de (06:30 TSİ) TBMM'nin sitelerinden kendiliğinden güncelleniyor.

> Eski adres (`tbmm.gov.tr/tutanak/tutanaklar.htm`) kapandığı için 2020'deki ilk sürümün indirme kodu artık çalışmıyor ([#1](https://github.com/ttoygar/meclis_genel_kurul_tutanaklari/issues/1)). O sürümün verisi ve anlatımı [aşağıda](#eski-yöntem-beautifulsoup4-ile-indirme) duruyor.

## Kapsam

> Şu an `data/` altında TBMM Genel Kurulu'nun dönem 20'den bugüne tutanakları (yeni site) var. Tablodaki diğer meclisler ve dönemler TBMM Kütüphanesi PDF arşivinden gelecek; crawler bunları okuyabiliyor ama arşiv ~76 GB olduğu için toplu indirme sonraya bırakıldı (`-kaynak www5`).

| Meclis | Yıllar | Klasör (`data/` altında) |
| --- | --- | --- |
| Meclis-i Mebusan | 1908–1920 | `meclisler/meclis-i-mebusan` |
| Meclis-i Ayan | 1908–1920 | `meclisler/meclis-i-ayan` |
| TBMM Genel Kurulu | 1920–1960, 1983–bugün | `tutanaklar` |
| TBMM Gizli Celse | 1920–2011 (bazı dönemler) | `meclisler/tbmm-gizli-celse` |
| Milli Birlik Komitesi | 1960–1961 | `meclisler/milli-birlik-komitesi` |
| Kurucu Meclis | 1961 | `meclisler/kurucu-meclis` |
| Temsilciler Meclisi | 1961 | `meclisler/temsilciler-meclisi` |
| Millet Meclisi | 1961–1980 | `meclisler/millet-meclisi` |
| Cumhuriyet Senatosu | 1961–1980 | `meclisler/cumhuriyet-senatosu` |
| TBMM Birleşik Toplantısı (MM - CS) | 1961–1980 | `meclisler/tbmm-birlesik-toplanti` |
| Milli Güvenlik Konseyi | 1980–1983 | `meclisler/milli-guvenlik-konseyi` |
| Danışma Meclisi | 1981–1983 | `meclisler/danisma-meclisi` |

## Klasörler

| Yol | İçerik |
| --- | --- |
| `data/tutanaklar/YYYY-MM-DD.txt` | TBMM Genel Kurulu birleşim metinleri. |
| `data/meclisler/<meclis>/YYYY-MM-DD.txt` | Diğer meclisler ve gizli celseler (klasör adları yukarıdaki tabloda). |
| `data/tutanaklar.csv`, `data/tutanaklar.json` | Her birleşimin bilgisi ve kaynak linkleri (alanlar aşağıda). |
| [Releases](../../releases) | Özgün html / docx / pdf dosyaları, yasama yılı başına bir release. |
| `tutanaklar/DD-MM-YYYY.txt` | 2020'deki ilk sürümün metinleri (dönem 20 – dönem 27 / 3. yasama yılı). Dokunulmadı. |
| `tutanaklar/archive/` | İlk sürümün defteri (`meclis_konusmalari.ipynb`) ve yardımcı dosyaları (`urls_dict.txt`, `tamamlanan_urller.txt`). |

Bir günde aynı klasörde birden fazla birleşim varsa ilki `YYYY-MM-DD.txt`, diğerleri `YYYY-MM-DD_<ek>.txt` adını alır (ör. `2026-11-03_d28y5b012.txt`, `1965-03-02_mm02003045.txt`). Hangi dosyanın hangi birleşim olduğu manifestte yazıyor.

### Manifest alanları

| Alan | Açıklama |
| --- | --- |
| `id` | Yeni sitede tutanak GUID'i, PDF arşivinde dosya yolu (ör. `tbmm/d19/c001/tbmm19001001`). |
| `meclis` | `TBMM`, `MM`, `CS`, `TBMM-BIRLESIK`, `GZC`, `MMB`, `MA`, `KM`, `TM`, `MBK`, `DM`, `MGK`. |
| `kaynak` | `tbmm.gov.tr` (yeni site) ya da `www5` (basılmış tutanak dergisi PDF arşivi). |
| `donem`, `yasama_yili`, `birlesim`, `cilt` | Cumhuriyet Senatosu ve birleşik toplantıda `yasama_yili` toplantı yılıdır, Mebusan/Ayan'da içtimadır (fevkalade içtima `0`). |
| `tarih` | `YYYY-MM-DD`. Rumi tarihler miladiye çevrildi. |
| `durum` | `Ham`, `Tam` ya da `Basılmış`. Ham tutanak sonradan Tam/Basılmış olunca metin yeniden indirilir. |
| `dosya` | `data/` altındaki txt yolu. |
| `metin_kaynagi` | Metnin çıkarıldığı dosya: `html`, `docx` ya da `pdf`. `yok`: dosyada metin katmanı yok (taranmış görüntü). |
| `html`, `docx`, `pdf`, `basilmis_pdf`, `gundem_pdf`, `eski_harfli_pdf` | Kaynak linkleri. `eski_harfli_pdf` 1920-28 tutanaklarının Osmanlıca baskısı. |
| `wayback` | Dosya TBMM sunucusunda bulunamayıp Wayback Machine'den alındıysa `true`. |
| `release_etiket` | Kaynak dosyaların bulunduğu release, ör. `kaynak-tbmm-d28-y5`, `kaynak-cs-t05`, `kaynak-mmb-d03-i01`. |

## Kaynaklar ve yöntem

- **Yeni TBMM sitesi** ([Tutanak Metinleri](https://www.tbmm.gov.tr/Tutanaklar/TutanakMetinleri)): TBMM dönem 20'den bugüne. Metin html'den, olmazsa docx'ten, o da olmazsa pdf'ten çıkarılır.
- **TBMM Kütüphanesi PDF arşivi** ([www5](https://www5.tbmm.gov.tr/develop/owa/tutanak_dergisi_pdfler.meclis_donemleri?v_meclisdonem=0), [Mebusan/Ayan](https://www5.tbmm.gov.tr/develop/owa/tutanak_dergisi_pdfler_mmb.meclis_donemleri?v_meclisdonem=0)): diğer bütün meclisler ve dönemler. Metin PDF'teki OCR katmanından geliyor; eski baskılarda OCR hataları var. Dönem 20 ve sonrasından yalnızca yeni sitede bulunmayan birleşimler alınır (ör. dönem 20 / 1. yasama yılının ilk 37 birleşimi).
- Bir dosya TBMM sunucusundan silinmişse [Wayback Machine](https://web.archive.org/)'deki kopyası denenir.
- Millet Meclisi, Danışma Meclisi, MGK, Kurucu/Temsilciler Meclisi ve MBK'da arşiv yasama yılı vermediği için yasama yılı tarihten hesaplanır (dönemin ilk birleşiminden sonra her 1 Kasım'da, 1981'den itibaren 1 Ekim'de yeni yıl başlar).
- Site cevap vermezse ya da sayfa yapısı değişirse güncelleme hata verir, eldeki veriyi ezmez. İş yarıda kalırsa bir sonraki çalıştırma kaldığı yerden devam eder.

## Elle çalıştırma

Go 1.24+ ve PDF metni için `pdftotext` ([Poppler](https://poppler.freedesktop.org/); Ubuntu'da `poppler-utils`, Windows'ta Git for Windows ile geliyor) gerekir. Repo kökünden:

```sh
go run ./crawler                        # yeni ya da durumu değişen tutanakları indir
go run ./crawler -liste                 # sadece ne yapılacağını göster
go run ./crawler -meclis MM -ornek 3    # deneme: Millet Meclisi'nden 3 birleşim
go run ./crawler -kaynak tbmm -donem 28 # sadece yeni site, dönem 28
go run ./crawler -release               # kaynakları Release'lere de yükle (gh CLI ve yetki gerekir)
```

Siteye yapılan istekler varsayılan olarak `tutanak_cache/` klasöründe saklanır; aynı sayfa ya da dosya bir daha istenince siteye gidilmez (geliştirme için, `.gitignore`'da). Liste sayfaları da saklandığından güncel veri için `-cache ""` ile kapatın ya da klasörü silin. Klasör `CRAWLER_CACHE` ile değiştirilebilir.

Kod [`crawler/`](crawler) klasöründe: `tbmm.go` yeni site, `www5.go` PDF arşivi, `extract.go` metin çıkarma, `release.go` Release yükleme. Workflow: [`.github/workflows/guncelle.yml`](.github/workflows/guncelle.yml).

---

## Eski yöntem: BeautifulSoup4 ile indirme

> 2020'deki ilk sürüm. Defter ve yardımcı dosyalar [`tutanaklar/archive/`](tutanaklar/archive) klasörüne taşındı; aşağıdaki kod eski adres kapandığı için artık çalışmıyor.

Bu programın ve defterin amacı daha sonra çeşitli NLP ve görselleştirme projelerinde kullanılmak üzere TBMM Genel Oturum tutanaklarının indirilmesi ve kaydedilmesi işlemlerini göstermektir. 

İndirilmiş tutanaklar oturumun gerçekleştiği günün tarihi ile adlandırılmış olarak __tutanaklar__ klasörünün içindedir.


```python
import requests
from bs4 import BeautifulSoup
import time
import random
```


TBMM'nin tutanaklar sayfasından istediğimiz linkleri çekerek urls_1 listesine kaydettik. İki seviye inmemiz gerektiğinden, bu liste aracılığıyla bir alt seviyedeki linklere ulaşıp urls_dict sözlüğünü oluşturduk. Bu sözlüğü herhangi bir aksilik oluşması riskine karşılık "urls_dict.txt" dosyasına kaydettik.


```python
ilk_hedef = requests.get("https://www.tbmm.gov.tr/tutanak/tutanaklar.htm").content
soup1=BeautifulSoup(ilk_hedef)

urls_1=[]
for i in soup1.find_all("a", href=True):
    urls_1.append(str(i).split('="')[1].split('" s')[0])

    
    # urls_1 listesini kullanarak istediğimiz linkleri urls_dict sözlüğüne kaydediyoruz; 
urls_dict = {}

for url in urls_1:
    ikinci_hedef = requests.get(url).content
    soup2 = BeautifulSoup(ikinci_hedef)

    # İstediğimiz özelliği taşımayan linkleri ayıklıyoruz;
    for i in soup2.find_all("a", href=True):
        if (not "Özet" in i) and (not "Açık Oylama Sonuçları" in i) and (not "Sesli Özet" in i) and (not "İşaret Dili" in i):             
            j = str(i).split('="')[1].split('">')[0]
            k = str(i.parent.next_sibling.next_sibling).split("d>")[1].split(" ")[0].replace(".","-")
            if ("ham" in j) or ("bas" in j):
                urls_dict[k] = j
      
"""
# SÖZLÜĞÜ YEDEKLEMEK AMACIYLA "urls_dict", 'urls_dict.txt' DOSYASINA KAYDEDİLDİ
with open('urls_dict.txt', 'w') as f:
    print(urls_dict, file=f)  
"""

    # Yedeklemek Önemli;
urls_yedek = urls_dict.copy()         
```

İşlemin yarıda kesilmesi riskine karşılık, "tamamlanan_urller.txt" adlı bir dosya oluşturarak işlemi tamamlanmış linkleri bu dosyaya kaydettik. İşlemin sekteye uğraması halinde bu dosyaya kaydedilmiş linkler kontrol edilip dosyada bulunmayan linkler işleme sokulabilir.


```python

counter = 0

    # urls_dict sözlüğündeki her bir elemanın içeriğinin okunması;
for key,value in urls_dict.items():
    with open("tamamlanan_urller.txt", 'r') as f:
        temp=list(set(f.readlines()))
        
    # Satır sonlarında bulunan "\n"(newline) karakterlerini temizledik;
        for i in range(len(temp)):
            temp[i]=temp[i].strip("\n")
            
    # Daha önce kaydedilmemiş sayfaların indirilmesi;
        if value not in temp:
            req = requests.get(value).content
            soup = BeautifulSoup(req)
            text = soup.get_text().replace("\xa0","")
            with open(f"tutanaklar/{key}.txt", 'wb') as f:
                f.write(text.encode("utf-8"))

    # İşlem yarıda kesilirse tamamlanmış kısımlar kaybedilmesin diye;
            with open("tamamlanan_urller.txt", 'a') as f:      
                f.write(value)
                f.write("\r\n")

            counter+=1
            
    # Siteyi Boğmamak için;
            time.sleep(random.randint(10,30))                    
    
# Son olarak, indirilen sayfa sayısını görmek için;
print(f'{counter} adet sayfa indirildi ve kaydedildi.')
    
```

    3017 adet sayfa indirildi ve kaydedildi.
    

Bundan sonrası için ayrı ayrı kaydedilmiş txt dosyalarının düzenlenmesi ve ayıklanması gerekli. 

Düşünceler;
* "urls_1" ve "urls_dict"i ayırmaya gerek olmayabilirdi. 
* Markdown kısımlarının doldurulmasının sona bırakılmaması gerekir. 
* Program bu haliyle yeni eklenen tutanakların veri tabanına eklenmesi için de kullanılabilir. 
