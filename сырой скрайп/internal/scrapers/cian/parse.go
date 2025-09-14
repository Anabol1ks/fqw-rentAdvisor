package cian

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// JSON-LD модель (совместима с типовой разметкой на ЦИАН)
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
	candidates := make([]JSONLD, 0, 8)
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		// На ЦИАН иногда массив объектов
		if strings.HasPrefix(raw, "[") {
			var arr []map[string]any
			if json.Unmarshal([]byte(raw), &arr) == nil {
				for _, m := range arr {
					b, _ := json.Marshal(m)
					var j JSONLD
					if json.Unmarshal(b, &j) == nil {
						if j.Name != "" || j.Description != "" || j.Address.StreetAddress != "" {
							candidates = append(candidates, j)
						}
					}
				}
			}
			return
		}
		var j JSONLD
		if json.Unmarshal([]byte(raw), &j) == nil {
			if j.Name != "" || j.Description != "" || j.Address.StreetAddress != "" {
				candidates = append(candidates, j)
			}
		}
	})

	// Выбираем «лучший» по наличию ключевых полей
	bestIdx := -1
	bestScore := -1 << 30
	score := func(j JSONLD) int {
		sc := 0
		if j.Name != "" {
			sc += 10
		}
		if j.Description != "" {
			sc += 5
		}
		if j.Address.StreetAddress != "" || j.Address.AddressLocal != "" {
			sc += 7
		}
		if j.Geo.Latitude != nil && j.Geo.Longitude != nil {
			sc += 8
		}
		if j.Offers.Price != "" {
			sc += 6
		}
		return sc
	}
	for i, c := range candidates {
		sc := score(c)
		if sc > bestScore {
			bestScore = sc
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		return candidates[bestIdx], true
	}
	return JSONLD{}, false
}

func normalizeCurrency(cur string) string {
	c := strings.ToUpper(strings.TrimSpace(cur))
	switch c {
	case "RUB", "RUR", "РУБ", "₽", "RUBLES":
		return "RUB"
	case "USD", "$":
		return "USD"
	case "EUR", "€":
		return "EUR"
	default:
		if c == "" {
			return "RUB"
		}
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

func parseInt(s string) *int {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.Fields(s)[0]
	if n, err := strconv.Atoi(s); err == nil {
		return &n
	}
	return nil
}

// extractAddressAndPointFromHTML: обобщённый поиск streetAddress и point{lat,lon}
func extractAddressAndPointFromHTML(html string) (street string, lat *float64, lon *float64) {
	streetRe := regexp.MustCompile(`"streetAddress"\s*:\s*"([^"]+)"`)
	pos := streetRe.FindStringSubmatchIndex(html)
	if len(pos) >= 4 {
		street = html[pos[2]:pos[3]]
		// по соседству координаты
		from := pos[0] - 2500
		if from < 0 {
			from = 0
		}
		to := pos[1] + 2500
		if to > len(html) {
			to = len(html)
		}
		window := html[from:to]
		pointRe := regexp.MustCompile(`"(point|geo|location)"\s*:\s*\{[^}]*?("latitude"\s*:\s*([0-9]+(?:\.[0-9]+)?))[^}]*?("longitude"\s*:\s*([0-9]+(?:\.[0-9]+)?))`)
		m := pointRe.FindStringSubmatch(window)
		if len(m) >= 6 {
			lat = parseFloat(m[3])
			lon = parseFloat(m[5])
		}
	}

	if street == "" {
		// Фолбэк по другим полям адреса
		oneline := regexp.MustCompile(`"unifiedOneline"\s*:\s*"([^"]+)"`).FindStringSubmatch(html)
		if len(oneline) >= 2 {
			street = oneline[1]
		} else {
			// components
			compRe := regexp.MustCompile(`"component"\s*:\s*\{\s*"name"\s*:\s*"([^"]+)"`)
			comps := compRe.FindAllStringSubmatch(html, -1)
			parts := make([]string, 0, len(comps))
			for _, c := range comps {
				if len(c) >= 2 {
					parts = append(parts, c[1])
				}
			}
			street = strings.Join(parts, ", ")
		}
	}

	if lat == nil || lon == nil {
		// Вариант с latitude/longitude
		pointRe := regexp.MustCompile(`"(point|geo|location)"\s*:\s*\{[^}]*?"latitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*"longitude"\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
		if m := pointRe.FindStringSubmatch(html); len(m) >= 4 {
			lat = parseFloat(m[2])
			lon = parseFloat(m[3])
		}
	}
	if lat == nil || lon == nil {
		// Вариант с coordinates { lat, lng }
		coordsRe := regexp.MustCompile(`"coordinates"\s*:\s*\{[^}]*?"lat"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*"lng"\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
		if m := coordsRe.FindStringSubmatch(html); len(m) >= 3 {
			lat = parseFloat(m[1])
			lon = parseFloat(m[2])
		}
	}
	street = strings.TrimSpace(strings.Trim(street, ", "))
	return
}

// extractFloorsFromInlineJSON — достаёт этаж и этажность из встроенного JSON блока offerdata
func extractFloorsFromInlineJSON(html string) (floor *int, floorsTotal *int) {
	// Предпочтительно: offerdata.offer.floornumber
	if m := regexp.MustCompile(`(?i)"floornumber"\s*:\s*(\d+)`).FindStringSubmatch(html); len(m) >= 2 {
		floor = parseInt(m[1])
	}
	// Этажность: building.floorscount или bti.housedata.floormax
	if m := regexp.MustCompile(`(?i)"floorscount"\s*:\s*(\d+)`).FindStringSubmatch(html); len(m) >= 2 {
		floorsTotal = parseInt(m[1])
	} else if m2 := regexp.MustCompile(`(?i)"floormax"\s*:\s*(\d+)`).FindStringSubmatch(html); len(m2) >= 2 {
		floorsTotal = parseInt(m2[1])
	}
	return
}

// extractClosestMetroFromInlineJSON — выбирает метро с минимальным traveltime из offerdata.offer.undergrounds
func extractClosestMetroFromInlineJSON(html string) (name string, minutes *int) {
	// Выделим блок undergrounds [...]
	reBlock := regexp.MustCompile(`(?is)"undergrounds"\s*:\s*\[(.*?)\]`)
	b := reBlock.FindStringSubmatch(html)
	if len(b) < 2 {
		return "", nil
	}
	block := b[1]
	// Найдём пары name + traveltime во всём блоке
	reItem := regexp.MustCompile(`(?is)\{[^\}]*?"name"\s*:\s*"([^"]+)"[^\}]*?"traveltime"\s*:\s*(\d+)[^\}]*?\}`)
	items := reItem.FindAllStringSubmatch(block, -1)
	bestName := ""
	bestMin := 1 << 30
	for _, it := range items {
		if len(it) < 3 {
			continue
		}
		n := strings.TrimSpace(it[1])
		v, _ := strconv.Atoi(it[2])
		if v < bestMin {
			bestMin = v
			bestName = n
		}
	}
	if bestName != "" {
		return bestName, &bestMin
	}
	return "", nil
}

// extractFactsFromText — те же правила, что и в яндекс-скраперe (свести к тексту)
func extractFactsFromText(title, desc string) (rooms *int, areaTotal *float64, floor *int, floorsTotal *int) {
	text := strings.ToLower(title + "\n" + desc)

	// студия → 0
	studioRe := regexp.MustCompile(`(?m)(^|[^\p{L}\p{N}])студи(?:я|ю|и|ей|е|о)($|[^\p{L}\p{N}])`)
	if studioRe.FindStringIndex(text) != nil {
		z := 0
		rooms = &z
	} else {
		// 1-6 комнат: поддержим "-комн.", "-комнатн", "комн" и т.п.
		numRe := regexp.MustCompile(`(?m)(^|[^0-9])(1|2|3|4|5|6)\s*(?:[\-—‑–]?\s*комн\.?|[\-—‑–]?\s*комнатн\w*|[\-—‑–]?\s*к(?:омнатн\w*|\.|\s*кв)|\s*комн\.?|\s*комнаты?)`)
		if m := numRe.FindStringSubmatch(text); len(m) >= 3 {
			n, _ := strconv.Atoi(m[2])
			rooms = &n
		} else {
			// словесные
			words := map[string]int{"двух": 2, "трех": 3, "трёх": 3, "четыр": 4, "пят": 5, "шест": 6}
			for w, n := range words {
				if strings.Contains(text, w+"комнатн") || strings.Contains(text, w+" комнатн") {
					rooms = &n
					break
				}
			}
		}
	}

	// общая площадь
	areaRe := regexp.MustCompile(`(?i)(общая\s*площад[ь|и]|площадь)\s*[:\-]?\s*([0-9]+(?:[\.,][0-9]+)?)\s*м`)
	if m := areaRe.FindStringSubmatch(text); len(m) >= 3 {
		areaTotal = parseFloat(m[2])
	} else {
		// Часто у ЦИАН в заголовке «159,5 м²», возьмём первое число с «м²»
		simple := regexp.MustCompile(`([0-9]+(?:[\.,][0-9]+)?)\s*м²`)
		if m := simple.FindStringSubmatch(text); len(m) >= 2 {
			areaTotal = parseFloat(m[1])
		}
	}

	// этаж / этажность
	floorRe := regexp.MustCompile(`(?i)(на\s*)?([0-9]+)\s*этаж[еа]?(\s*из\s*([0-9]+))?`)
	if m := floorRe.FindStringSubmatch(text); len(m) >= 3 {
		floor = parseInt(m[2])
		if len(m) >= 5 && m[4] != "" {
			floorsTotal = parseInt(m[4])
		}
	}
	return
}

// extractMetroFromDoc — простой поиск «м. <станция>» + минуты по тексту страницы
func extractMetroFromDoc(doc *goquery.Document) string {
	// 1) Приоритет: блок с метро на карточке (берём ближайшее по минутам либо первый)
	if name, mins := extractClosestMetroFromUndergroundList(doc); name != "" {
		if mins != nil {
			return strings.TrimSpace(name + " " + strconv.Itoa(*mins) + " мин")
		}
		return name
	}
	// 2) Фолбэк по общему тексту (попробуем выцепить и название, и минуты)
	raw := strings.TrimSpace(doc.Text())
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\u00A0", " ")
	raw = strings.ReplaceAll(raw, "\u202F", " ")
	// Шаблон: "м. <станция> NN мин" или "метро <станция> NN мин"
	reWithMin := regexp.MustCompile(`(?i)(?:\bметро\b|\bм\.)\s*([A-ЯЁа-яё0-9\-\s]+?)\s*(\d{1,2})\s*мин`)
	if m := reWithMin.FindStringSubmatch(raw); len(m) >= 3 {
		name := strings.TrimSpace(m[1])
		mins := m[2]
		// зачистим возможные хвосты в имени
		name = strings.TrimSuffix(name, ",")
		name = strings.TrimSpace(strings.Split(name, " - база ЦИАН")[0])
		name = strings.TrimSpace(strings.Split(name, " — база ЦИАН")[0])
		return strings.TrimSpace(name + " " + mins + " мин")
	}
	// Фолбэк по одному названию без минут (как раньше, но без префикса "м.")
	reNameOnly := regexp.MustCompile(`(?i)(?:\bметро\b|\bм\.)\s*([A-ЯЁа-яё0-9\-\s,\(\)]+)`) // без lookahead
	if m := reNameOnly.FindStringSubmatch(raw); len(m) >= 2 {
		frag := m[1]
		cutDelims := []string{",", "(", " - ", " — ", " минут", " мин", " пешк", " на трансп"}
		end := len(frag)
		low := strings.ToLower(frag)
		for _, d := range cutDelims {
			if i := strings.Index(low, d); i >= 0 && i < end {
				end = i
			}
		}
		name := strings.TrimSpace(frag[:end])
		name = strings.TrimSuffix(name, ",")
		name = strings.TrimSpace(strings.Split(name, " - база ЦИАН")[0])
		name = strings.TrimSpace(strings.Split(name, " — база ЦИАН")[0])
		return name
	}
	return ""
}

// extractAreasFromDoc — ищем по лейблам «Жилая», «Кухня» рядом с значениями
func extractAreasFromDoc(doc *goquery.Document) (areaLiving *float64, areaKitchen *float64) {
	text := strings.ToLower(doc.Text())
	text = strings.ReplaceAll(text, "\u00A0", " ")
	text = strings.ReplaceAll(text, "\u202F", " ")
	// жилая площадь
	liveRe := regexp.MustCompile(`(?i)жилая\s*(площад[ь|и])?\s*[:\-]?\s*([0-9]+(?:[\.,][0-9]+)?)\s*м`)
	if m := liveRe.FindStringSubmatch(text); len(m) >= 3 {
		areaLiving = parseFloat(m[2])
	}
	// площадь кухни
	kitRe := regexp.MustCompile(`(?i)(площадь\s*кухни|кухн[яи])\s*[:\-]?\s*([0-9]+(?:[\.,][0-9]+)?)\s*м`)
	if m := kitRe.FindStringSubmatch(text); len(m) >= 3 {
		areaKitchen = parseFloat(m[2])
	}
	return
}

// extractHouseInfoFromDoc — «Год постройки/сдачи» + «Материал стен» по тексту страницы
func extractHouseInfoFromDoc(doc *goquery.Document) (yearBuilt *int, material string) {
	// 1) DOM-лейбл «Тип дома» в блоке характеристик
	material = extractHouseMaterialFromDOM(doc)
	// 2) Год постройки/сдачи и текстовые фолбэки
	raw := strings.ToLower(doc.Text())
	raw = strings.ReplaceAll(raw, "\u00A0", " ")
	raw = strings.ReplaceAll(raw, "\u202F", " ")
	yearBuilt = findYearNearLabel(raw)
	if material == "" {
		// Текстовый фолбэк по «Тип дома»/«Материал стен»
		typeRe := regexp.MustCompile(`(?i)тип\s*дома\s*[:\-]?\s*([А-ЯЁа-яё\-\s]+)\b`)
		if m := typeRe.FindStringSubmatch(raw); len(m) >= 2 {
			material = normalizeHouseMaterial(m[1])
		}
		if material == "" {
			matRe := regexp.MustCompile(`(?i)(материал\s*стен|конструкци[ия]).{0,40}?([А-ЯЁа-яё\-\s]+)\b`)
			if m := matRe.FindStringSubmatch(raw); len(m) >= 3 {
				material = normalizeHouseMaterial(m[2])
			}
		}
	}
	return
}

// findYearNearLabel ищет год только рядом с лейблами "год постройки/сдачи/построен/сдан"
func findYearNearLabel(s string) *int {
	re := regexp.MustCompile(`(?i)(год\s*(постройки|сдачи)|построен[а]?|сдан[а]?)\D{0,20}(19\d{2}|20\d{2}|2100)`)
	if m := re.FindStringSubmatch(s); len(m) >= 4 {
		if n, err := strconv.Atoi(m[3]); err == nil {
			return &n
		}
	}
	return nil
}

// normalizeHouseMaterial — максимально совместимо с яндекс-скрапером
func normalizeHouseMaterial(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.Join(strings.Fields(s), " ")

	if s == "" {
		return ""
	}
	// ключевые слова
	has := func(substr string) bool { return strings.Contains(s, substr) }

	// Комбинированные
	if has("монолит") && has("кирп") {
		return "монолит-кирпич"
	}
	if has("панел") && has("кирп") {
		return "панель-кирпич"
	}

	// Простые варианты
	switch {
	case has("кирп"):
		return "кирпич"
	case has("монолит"):
		return "монолит"
	case has("панел"):
		return "панель"
	case has("блок"):
		return "блок"
	case has("дерев") || has("брус"):
		return "дерево"
	case has("газобет") || has("пенобет") || has("аэробел"):
		return "газобетон"
	}
	return ""
}

// extractHouseMaterialFromDOM — ищет «Тип дома» в карточке и возвращает нормализованное значение
func extractHouseMaterialFromDOM(doc *goquery.Document) string {
	// Ищем пары label/value в блоках характеристик
	// Например: <p>Тип дома</p><p>Монолитный</p>
	var result string
	doc.Find("div, section, li").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		// Ищем текстовый узел лейбла «Тип дома» внутри контейнера
		hasLabel := false
		s.Find("*").EachWithBreak(func(_ int, n *goquery.Selection) bool {
			t := strings.TrimSpace(n.Text())
			if t == "Тип дома" {
				hasLabel = true
				return false
			}
			return true
		})
		if !hasLabel {
			return true
		}
		// Попробуем найти ближайший следующий текст, который выглядит как значение
		// Простая эвристика: второй <p> подряд, либо соседний элемент с непустым текстом
		// Сначала попробуем найти прямого соседа по DOM
		s.Find("p, span, div").EachWithBreak(func(_ int, n *goquery.Selection) bool {
			text := strings.TrimSpace(n.Text())
			if text != "" && text != "Тип дома" {
				result = normalizeHouseMaterial(text)
				return false
			}
			return true
		})
		return result == ""
	})
	return result
}

// extractFloorFromDoc — ищет этаж/этажность по общему тексту страницы
func extractFloorFromDoc(doc *goquery.Document) (floor *int, floorsTotal *int) {
	raw := strings.ToLower(doc.Text())
	raw = strings.ReplaceAll(raw, "\u00A0", " ")
	raw = strings.ReplaceAll(raw, "\u202F", " ")
	// Вариант «4 этаж (из 28)»
	re := regexp.MustCompile(`(?i)(?:на\s*)?(\d+)\s*этаж[еа]?(?:\s*из\s*(\d+))?`)
	if m := re.FindStringSubmatch(raw); len(m) >= 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			floor = &n
		}
		if len(m) >= 3 && m[2] != "" {
			if t, err := strconv.Atoi(m[2]); err == nil {
				floorsTotal = &t
			}
		}
	}
	// Вариант «Этаж 4 из 28»
	if floor == nil {
		re2 := regexp.MustCompile(`(?i)этаж\s*(\d+)(?:\s*из\s*(\d+))?`)
		if m := re2.FindStringSubmatch(raw); len(m) >= 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				floor = &n
			}
			if len(m) >= 3 && m[2] != "" {
				if t, err := strconv.Atoi(m[2]); err == nil {
					floorsTotal = &t
				}
			}
		}
	}
	if floor == nil {
		// Словесные формы: на первом/втором/третьем/четвертом ... этаже
		// Будем искать корни слов, чтобы покрыть склонения
		ord := map[string]int{
			"перв": 1, "втор": 2, "трет": 3, "треть": 3, "четверт": 4, "пят": 5,
			"шест": 6, "седьм": 7, "восьм": 8, "девят": 9, "десят": 10,
			"одиннадц": 11, "двенадц": 12, "тринадц": 13, "четырнадц": 14, "пятнадц": 15,
			"шестнадц": 16, "семнадц": 17, "восемнадц": 18, "девятнадц": 19, "двадц": 20,
		}
		rew := regexp.MustCompile(`(?i)на\s+([а-яё]+?)\s+этаж`)
		if m := rew.FindStringSubmatch(raw); len(m) >= 2 {
			w := m[1]
			for k, v := range ord {
				if strings.HasPrefix(w, k) {
					vv := v
					floor = &vv
					break
				}
			}
		}
	}
	return
}

// extractAddressFromTitle — берёт адрес из <title>, обрезая служебные хвосты
func extractAddressFromTitle(doc *goquery.Document) string {
	t := strings.TrimSpace(doc.Find("title").First().Text())
	if t == "" {
		return ""
	}
	// Часто в тайтле присутствует хвост "- база ЦИАН" — срежем его
	cut := strings.Split(t, " - база ЦИАН")[0]
	cut = strings.Split(cut, " — база ЦИАН")[0]
	cut = strings.Split(cut, ", объявление")[0]
	// Эвристика: ищем начало адреса по ключевым индикаторам
	lowers := strings.ToLower(cut)
	starts := []string{"ул.", "улица", "просп.", "проспект", "ш.", "шоссе", "б-р", "бульвар", "пер.", "переулок", "проезд", "наб.", "набережная", "пл.", "площадь"}
	startIdx := -1
	for _, s := range starts {
		if i := strings.Index(lowers, s); i >= 0 {
			if startIdx == -1 || i < startIdx {
				startIdx = i
			}
		}
	}
	if startIdx >= 0 {
		candidate := strings.TrimSpace(cut[startIdx:])
		candidate = strings.Split(candidate, " м. ")[0]
		candidate = strings.Trim(candidate, ", ")
		return candidate
	}
	return ""
}

// extractAddressFromDoc — пытается вытащить адрес по лейблу или itemprop
func extractAddressFromDoc(doc *goquery.Document) string {
	// 1) Приоритет: адресная линия карточки
	if v := extractAddressFromCianAddressLine(doc); v != "" {
		return v
	}
	if v := strings.TrimSpace(doc.Find(`[itemprop="streetAddress"]`).First().Text()); v != "" {
		return strings.Trim(v, ", ")
	}
	raw := strings.ToLower(doc.Text())
	raw = strings.ReplaceAll(raw, "\u00A0", " ")
	raw = strings.ReplaceAll(raw, "\u202F", " ")
	re := regexp.MustCompile(`(?i)адрес\s*[:\-]?\s*([^\n]+)`)
	if m := re.FindStringSubmatch(raw); len(m) >= 2 {
		addr := strings.TrimSpace(m[1])
		addr = strings.Split(addr, " м. ")[0]
		addr = strings.Trim(addr, ", ")
		return addr
	}
	return ""
}

// extractAddressFromCianAddressLine — вытаскивает адрес из блока AddressContainer (цепочка ссылок)
func extractAddressFromCianAddressLine(doc *goquery.Document) string {
	sel := doc.Find(`div[data-name="AddressContainer"] a[data-name="AddressItem"]`)
	if sel.Length() == 0 {
		return ""
	}
	parts := make([]string, 0, sel.Length())
	sel.Each(func(_ int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if t != "" {
			parts = append(parts, t)
		}
	})
	addr := strings.Join(parts, ", ")
	addr = strings.Trim(addr, ", ")
	return addr
}

// extractClosestMetroFromUndergroundList — выбирает метро с минимальным временем ходьбы
func extractClosestMetroFromUndergroundList(doc *goquery.Document) (string, *int) {
	items := doc.Find(`ul[data-name="UndergroundList"] li[data-name="UndergroundItem"]`)
	if items.Length() == 0 {
		return "", nil
	}
	type candidate struct {
		name string
		mins int
		idx  int
	}
	best := candidate{"", 1 << 30, 1 << 30}
	items.Each(func(i int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find(`a.xa15a2ab7--d9f62d--underground_link, a[data-name="UndergroundLink"], a`).First().Text())
		if name == "" {
			return
		}
		// Время: ищем «NN мин.» в явных контейнерах, затем фолбэк по всему LI
		mins := 1 << 29
		reMin := regexp.MustCompile(`(\d+)\s*мин`)
		// прицельные селекторы времени
		timeSel := s.Find(`[data-name="UndergroundTime"], [class*="underground_time"], span, div`)
		found := false
		timeSel.EachWithBreak(func(_ int, tn *goquery.Selection) bool {
			timeText := strings.ToLower(strings.TrimSpace(tn.Text()))
			timeText = strings.ReplaceAll(timeText, "\u00A0", " ")
			timeText = strings.ReplaceAll(timeText, "\u202F", " ")
			if m := reMin.FindStringSubmatch(timeText); len(m) >= 2 {
				if v, err := strconv.Atoi(m[1]); err == nil {
					mins = v
					found = true
					return false
				}
			}
			return true
		})
		if !found {
			// фолбэк: весь текст элемента li
			allText := strings.ToLower(strings.TrimSpace(s.Text()))
			allText = strings.ReplaceAll(allText, "\u00A0", " ")
			allText = strings.ReplaceAll(allText, "\u202F", " ")
			if m := reMin.FindStringSubmatch(allText); len(m) >= 2 {
				if v, err := strconv.Atoi(m[1]); err == nil {
					mins = v
				}
			}
		}
		cand := candidate{name: name, mins: mins, idx: i}
		// Выбор по минимальному времени, затем по порядку
		if cand.mins < best.mins || (cand.mins == best.mins && cand.idx < best.idx) {
			best = cand
		}
	})
	if best.name != "" {
		if best.mins < (1 << 29) {
			return best.name, &best.mins
		}
		return best.name, nil
	}
	// Фолбэк: первый элемент
	first := strings.TrimSpace(items.First().Find(`a`).First().Text())
	if first != "" {
		return first, nil
	}
	return "", nil
}
