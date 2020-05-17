# TBMM Genel Kurul Tutanaklarının BeautifulSoup4 ile indirilmesi

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
