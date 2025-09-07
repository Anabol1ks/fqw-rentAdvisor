package yandex

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var idRe = regexp.MustCompile(`(\d{6,})`) // из URL или из data-атрибутов

type JSONLD struct {
	Type        string `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Offers      struct {
		Price    json.Number `json:"price"`
		PriceCur string      `json:"priceCurrency"`
	} `json:"offers"`
	Address struct {
		StreetAddress string `json:"streetAddress"`
		AddressLocal  string `json:"addressLocality"`
	} `json:"address"`
	Geo struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	} `json:"geo"`
}

func parseJSONLD(doc *goquery.Document) (JSONLD, bool) {
	// Соберём все кандидаты и выберем лучший по типу
	candidates := make([]JSONLD, 0, 8)
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		if strings.HasPrefix(raw, "[") {
			var arr []map[string]any
			if json.Unmarshal([]byte(raw), &arr) == nil {
				for _, m := range arr {
					b, _ := json.Marshal(m)
					var tmp JSONLD
					if json.Unmarshal(b, &tmp) == nil {
						candidates = append(candidates, tmp)
					}
				}
			}
			return
		}
		var tmp JSONLD
		if json.Unmarshal([]byte(raw), &tmp) == nil {
			candidates = append(candidates, tmp)
		}
	})

	bestIdx := -1
	bestScore := -1 << 30
	score := func(j JSONLD) int {
		s := 0
		t := strings.ToLower(strings.TrimSpace(j.Type))
		switch t {
		case "product":
			s += 100
		case "offer":
			s += 80
		case "apartment", "realestatelisting":
			s += 70
		case "organization", "breadcrumblist":
			s -= 1000
		}
		if j.Offers.Price != "" {
			s += 50
		}
		if j.Name != "" {
			s += 10
		}
		return s
	}
	for i, c := range candidates {
		sc := score(c)
		if sc > bestScore {
			bestIdx, bestScore = i, sc
		}
	}
	if bestIdx >= 0 && bestScore > -1000 { // не отдаём Organization/хлебные крошки
		return candidates[bestIdx], true
	}
	return JSONLD{}, false
}

func normalizeCurrency(cur string) string {
	c := strings.ToUpper(strings.TrimSpace(cur))
	switch c {
	case "RUB", "RUR", "₽":
		return "RUB"
	case "USD", "$":
		return "USD"
	case "EUR", "€":
		return "EUR"
	default:
		return c
	}
}

func parseFloat(s string) *float64 {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, ",", ".")
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return &f
	}
	return nil
}

func extractIDFromURL(u string) string {
	if m := idRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	// fallback — нормализованный URL без протокола и без крайних дефисов
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.Trim(u, "/")
	u = strings.ReplaceAll(u, "/", "-")
	u = strings.Trim(u, "-")
	return u
}

func extractAddressAndPointFromHTML(html string) (street string, lat *float64, lon *float64) {
	// 1) Найдём первый streetAddress
	streetRe := regexp.MustCompile(`"streetAddress"\s*:\s*"([^"]+)"`)
	pos := streetRe.FindStringSubmatchIndex(html)
	if len(pos) >= 4 {
		street = html[pos[2]:pos[3]]
		// 2) Вокруг найденного streetAddress ищем ближайший point{lat, lon} в окне +-2000 символов
		start := pos[0] - 2000
		if start < 0 {
			start = 0
		}
		end := pos[1] + 3000
		if end > len(html) {
			end = len(html)
		}
		window := html[start:end]
		pointRe := regexp.MustCompile(`"point"\s*:\s*\{\s*"latitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*"longitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
		if pm := pointRe.FindStringSubmatch(window); len(pm) == 3 {
			if f, err := strconv.ParseFloat(pm[1], 64); err == nil {
				lat = &f
			}
			if f, err := strconv.ParseFloat(pm[2], 64); err == nil {
				lon = &f
			}
		}
		return
	}

	// Фолбэк-адрес: structuredAddress.unifiedOneline или component -> соберём упрощённо
	if street == "" {
		if m := regexp.MustCompile(`"unifiedOneline"\s*:\s*"([^"]+)"`).FindStringSubmatch(html); len(m) == 2 {
			street = strings.TrimSpace(m[1])
		} else {
			// соберём valueForAddress/ value полей в одну линию
			compRe := regexp.MustCompile(`\{"value(?:ForAddress)?"\s*:\s*"([^"]+)"`)
			comps := compRe.FindAllStringSubmatch(html, 6)
			if len(comps) > 0 {
				parts := make([]string, 0, len(comps))
				for _, c := range comps {
					v := strings.TrimSpace(c[1])
					if v != "" {
						parts = append(parts, v)
					}
				}
				if len(parts) > 0 {
					street = strings.Join(parts, ", ")
				}
			}
		}
	}

	// Фолбэк: если streetAddress не нашли, попробуем взять просто первый point
	pointRe := regexp.MustCompile(`"point"\s*:\s*\{\s*"latitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*"longitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	if pm := pointRe.FindStringSubmatch(html); len(pm) == 3 {
		if f, err := strconv.ParseFloat(pm[1], 64); err == nil {
			lat = &f
		}
		if f, err := strconv.ParseFloat(pm[2], 64); err == nil {
			lon = &f
		}
	}
	return
}

// extractAddressAndPointFromDoc пытается достать видимый адрес и координаты из DOM:
// - координаты лежат в атрибутах data-latitude и data-longtitude/data-longitude
// - адрес — первый элемент с классом CardLocation__addressItem--*
func extractAddressAndPointFromDoc(doc *goquery.Document) (addr string, lat *float64, lon *float64) {
	// 1) Координаты
	found := false
	doc.Find("[data-latitude]").Each(func(i int, s *goquery.Selection) {
		if found {
			return
		}
		latS, okLat := s.Attr("data-latitude")
		if !okLat || strings.TrimSpace(latS) == "" {
			return
		}
		// Яндекс иногда пишет 'data-longtitude' (c буквой t), а не 'data-longitude'
		lonS, okLon := s.Attr("data-longtitude")
		if !okLon || strings.TrimSpace(lonS) == "" {
			if v, ok := s.Attr("data-longitude"); ok && strings.TrimSpace(v) != "" {
				lonS = v
				okLon = true
			}
		}
		if !okLon {
			return
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(latS), 64); err == nil {
			lat = &f
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(lonS), 64); err == nil {
			lon = &f
		}
		if lat != nil && lon != nil {
			found = true
		}
	})

	// 2) Адрес — видимый текст на карточке. Берём первый addressItem рядом
	if a := strings.TrimSpace(doc.Find(".CardLocation__addressWrapper--3CTMa .CardLocation__addressItem--1JYpZ").First().Text()); a != "" {
		addr = a
	} else if a := strings.TrimSpace(doc.Find(".CardLocation__addressItem--1JYpZ").First().Text()); a != "" {
		addr = a
	} else {
		// Более устойчивые варианты:
		//  - meta[name=description] содержит "Адрес: <...>, ✅"
		if metaDesc, exists := doc.Find(`meta[name="description"]`).Attr("content"); exists {
			re := regexp.MustCompile(`Адрес:\s*([^,]+(?:,\s*[^,✅]+)*)`)
			if m := re.FindStringSubmatch(metaDesc); len(m) == 2 {
				addr = strings.TrimSpace(m[1])
			}
		}
		//  - <title> содержит "— <адрес> — id ..."
		if addr == "" {
			title := strings.TrimSpace(doc.Find("title").Text())
			if title != "" {
				re := regexp.MustCompile(`—\s*([^—]+?)\s*—\s*id\s*\d+`)
				if m := re.FindStringSubmatch(title); len(m) == 2 {
					addr = strings.TrimSpace(m[1])
				}
			}
		}
	}

	// Финальная зачистка адреса
	addr = strings.TrimSpace(strings.Trim(addr, ", "))
	return
}

// extractFactsFromText парсит базовые характеристики из title/description:
// - rooms: из "1-комнатн" / "2 комн" и т.п.
// - areaTotal: первая встречающаяся "NN[,.]MM м²"
// - floor/floorsTotal: из "на 19 этаже из 29" или "19 этаж из 29"
func extractFactsFromText(title, desc string) (rooms *int, areaTotal *float64, floor *int, floorsTotal *int) {
	text := strings.ToLower(title + "\n" + desc)

	// rooms
	// примеры: "1-комнатную", "2-комнатная", "3 комн"
	if m := regexp.MustCompile(`(?m)(\d+)\s*[- ]?комнат`).FindStringSubmatch(text); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			rooms = &v
		}
	} else if m := regexp.MustCompile(`(?m)(\d+)\s*[- ]?комн`).FindStringSubmatch(text); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			rooms = &v
		}
	}

	// area total (м²)
	if m := regexp.MustCompile(`(?m)(\d+(?:[\.,]\d+)?)\s*м²`).FindStringSubmatch(text); len(m) == 2 {
		if f := parseFloat(m[1]); f != nil {
			areaTotal = f
		}
	}

	// floor / floors total
	// варианты: "на 19 этаже из 29" или "19 этаж из 29"
	if m := regexp.MustCompile(`на\s*(\d+)\s*этаж[е]?\s*из\s*(\d+)`).FindStringSubmatch(text); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			floor = &v
		}
		if v, err := strconv.Atoi(m[2]); err == nil {
			floorsTotal = &v
		}
	} else if m := regexp.MustCompile(`\b(\d+)\s*этаж[е]?\s*из\s*(\d+)`).FindStringSubmatch(text); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			floor = &v
		}
		if v, err := strconv.Atoi(m[2]); err == nil {
			floorsTotal = &v
		}
	}
	return
}

// extractMetroFromDoc пытается найти ближайшую станцию метро из блока MetroWithTime
func extractMetroFromDoc(doc *goquery.Document) string {
	sel := doc.Find(".MetroWithTime").First()
	if sel.Length() == 0 {
		return ""
	}
	raw := strings.TrimSpace(sel.Text())
	if raw == "" {
		return ""
	}
	// Оставим начало до первой цифры/запятой — обычно это название станции
	re := regexp.MustCompile(`^\s*([\p{L}\-\s]+?)\s*(?:[•·\-,\d]|$)`) // буквы/дефис/пробелы до разделителя
	if m := re.FindStringSubmatch(raw); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return raw
}

// extractAreasFromDoc ищет площади по текстовым лейблам: "жилая", "жилая площадь", "кухня", "площадь кухни"
func extractAreasFromDoc(doc *goquery.Document) (areaLiving *float64, areaKitchen *float64) {
	text := strings.ToLower(doc.Text())
	// Жилая площадь
	if m := regexp.MustCompile(`(?m)жилая(?:\s+площадь)?\s*[:–\-]?\s*(\d+(?:[\.,]\d+)?)\s*м²`).FindStringSubmatch(text); len(m) == 2 {
		if f := parseFloat(m[1]); f != nil {
			areaLiving = f
		}
	}
	// Площадь кухни
	if m := regexp.MustCompile(`(?m)(?:кухн(?:я|и)|площадь\s+кухни)\s*[:–\-]?\s*(\d+(?:[\.,]\d+)?)\s*м²`).FindStringSubmatch(text); len(m) == 2 {
		if f := parseFloat(m[1]); f != nil {
			areaKitchen = f
		}
	}
	return
}
