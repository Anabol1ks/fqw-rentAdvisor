package yandex

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"skripe/internal/repo"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/gocolly/colly/v2/proxy"
	"go.uber.org/zap"

	"github.com/PuerkitoBio/goquery"
)

type Options struct {
	DBRepo       *repo.RawRepository
	StartURLTmpl string // например: https://realty.yandex.ru/{city}/kupit/kvartira/?page={page}
	City         string
	StartPage    int
	Pages        int
	SnapshotDir  string
	ProxyURL     string // опционально
	DelayMin     time.Duration
	DelayMax     time.Duration
	Parallelism  int
	// Новые параметры управления завершением
	MaxItems             int // 0 = без лимита; при достижении — остановка скрапинга
	MaxEmptyListingPages int // 0 = игнорировать; >0 — остановить после N подряд страниц выдачи без ссылок на детали
}

var detailRe = regexp.MustCompile(`/offer/\d+/`)

func (o Options) buildPageURL(page int, log *zap.Logger) string {
	u := strings.ReplaceAll(o.StartURLTmpl, "{city}", url.PathEscape(o.City))
	u = strings.ReplaceAll(u, "{page}", fmt.Sprintf("%d", page))
	return u
}

func Run(o Options, log *zap.Logger) error {
	if o.SnapshotDir == "" {
		o.SnapshotDir = "snapshots/yandex"
	}
	_ = os.MkdirAll(o.SnapshotDir, 0o755)

	c := colly.NewCollector(
		colly.AllowedDomains("realty.yandex.ru", "www.realty.yandex.ru", "yandex.ru"),
		colly.Async(true),
		colly.MaxDepth(2),
	)
	extensions.RandomUserAgent(c)
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: max(1, o.Parallelism),
		RandomDelay: clampDuration(o.DelayMax, 0, time.Second*10),
		Delay:       clampDuration(o.DelayMin, 0, time.Second*5),
	})

	if o.ProxyURL != "" {
		pf, err := proxy.RoundRobinProxySwitcher(o.ProxyURL)
		if err != nil {
			log.Warn("proxy setup error: ", zap.Error(err))
		} else {
			c.SetProxyFunc(pf)
		}
	}

	// Счётчики/состояние
	var upserted int64 // успешно сохраненные карточки
	var shouldStop int64
	var mu sync.Mutex
	listingLinkCount := make(map[string]int) // URL страницы выдачи -> кол-во детальных ссылок
	currentPage := o.StartPage
	pagesVisited := 0
	emptyStreak := 0

	// 1) на страницах выдачи собираем ссылки на детали и считаем их
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href == "" || !detailRe.MatchString(href) {
			return
		}
		// учитываем только если это страница выдачи (не детальная)
		if detailRe.MatchString(e.Request.URL.Path) {
			return
		}
		// если уже решили останавливаться — не планируем деталей
		if atomic.LoadInt64(&shouldStop) == 1 {
			return
		}
		link := e.Request.AbsoluteURL(href)
		// инкремент счётчика для текущей страницы выдачи
		mu.Lock()
		listingLinkCount[e.Request.URL.String()]++
		mu.Unlock()
		_ = c.Visit(link)
	})

	// 2) обработчик карточки
	c.OnResponse(func(r *colly.Response) {
		if !strings.Contains(r.Headers.Get("Content-Type"), "text/html") {
			return
		}
		// детальная страница?
		if !detailRe.MatchString(r.Request.URL.Path) {
			return
		}
		// снапшот
		file := snapshotPath(o.SnapshotDir, r.Request.URL.String(), r.Body)
		if err := os.WriteFile(file, r.Body, 0o644); err != nil {
			log.Warn("save snapshot err: ", zap.Error(err))
		}

		// парсим DOM
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			log.Warn("goquery err: ", zap.Error(err))
			return
		}
		jld, _ := parseJSONLD(doc)

		// Фолбэк: если из JSON-LD не получили адрес или координаты, попробуем достать из inline JSON
		// Сформируем адрес из JSON-LD: отдаём street, затем locality, фильтруя пустые поля
		addrParts := make([]string, 0, 2)
		if s := strings.TrimSpace(jld.Address.StreetAddress); s != "" {
			addrParts = append(addrParts, s)
		}
		if s := strings.TrimSpace(jld.Address.AddressLocal); s != "" {
			addrParts = append(addrParts, s)
		}
		addrText := strings.Join(addrParts, ", ")
		lat := jld.Geo.Latitude
		lon := jld.Geo.Longitude
		// 2.1) Попробуем вытащить адрес/координаты из DOM (атрибуты data-latitude/longitude)
		if addrText == "" || lat == nil || lon == nil {
			if a2, flat, flon := extractAddressAndPointFromDoc(doc); a2 != "" || flat != nil || flon != nil {
				if strings.TrimSpace(addrText) == "" && a2 != "" {
					addrText = a2
				}
				if lat == nil && flat != nil {
					lat = flat
				}
				if lon == nil && flon != nil {
					lon = flon
				}
			}
		}
		// 2.2) Если не помогло — regex по inline JSON
		if addrText == "" || lat == nil || lon == nil {
			street, flat, flon := extractAddressAndPointFromHTML(string(r.Body))
			if strings.TrimSpace(addrText) == "" && street != "" {
				addrText = strings.TrimSpace(street)
			}
			if lat == nil && flat != nil {
				lat = flat
			}
			if lon == nil && flon != nil {
				lon = flon
			}
		}

		pageURL := r.Request.URL.String()
		externalID := extractIDFromURL(pageURL)

		// Финальная зачистка адреса: убрать висячие запятые и двойные пробелы
		addrText = strings.TrimSpace(strings.Trim(addrText, ", "))
		// Факты из текста и DOM (комнаты/площадь/этаж/метро)
		rRooms, rAreaTotal, rFloor, rFloors := extractFactsFromText(jld.Name, jld.Description)
		if rRooms == nil {
			if rr := extractRoomsFromDoc(doc); rr != nil {
				rRooms = rr
			}
		}
		metro := extractMetroFromDoc(doc)
		rAreaLiving, rAreaKitchen := extractAreasFromDoc(doc)

		item := repo.RawItem{
			Source:        "yandex",
			ExternalID:    externalID,
			URL:           pageURL,
			Title:         strings.TrimSpace(jld.Name),
			Description:   strings.TrimSpace(jld.Description),
			AddressText:   addrText,
			PriceCurrency: normalizeCurrency(jld.Offers.PriceCur),
			Lat:           lat,
			Lon:           lon,
			Rooms:         rRooms,
			AreaTotal:     rAreaTotal,
			AreaLiving:    rAreaLiving,
			AreaKitchen:   rAreaKitchen,
			Floor:         rFloor,
			FloorsTotal:   rFloors,
			Metro:         metro,
			Payload: map[string]any{
				"snapshot_path": file,
				"jsonld_raw":    jld,
			},
		}
		if jld.Offers.Price != "" {
			if f := parseFloat(string(jld.Offers.Price)); f != nil {
				item.PriceValue = f
			}
		}

		if err := o.DBRepo.UpsertListingRaw(context.Background(), item); err != nil {
			log.Warn("upsert raw err: ", zap.Error(err))
		} else {
			msg := fmt.Sprintf("upserted %s %s", item.Source, item.ExternalID)
			log.Info(msg)
			// инкремент и проверка лимита
			if o.MaxItems > 0 {
				if atomic.AddInt64(&upserted, 1) >= int64(o.MaxItems) {
					log.Info("max items reached, will stop scheduling new requests", zap.Int("max_items", o.MaxItems))
					atomic.StoreInt64(&shouldStop, 1)
				}
			}
		}
	})

	// 3) по окончании обработки любой страницы решаем, продолжать ли пагинацию выдачи
	c.OnScraped(func(r *colly.Response) {
		// интересуют только страницы выдачи (не деталки)
		if detailRe.MatchString(r.Request.URL.Path) {
			return
		}
		// обновим streak пустых страниц
		mu.Lock()
		cnt := listingLinkCount[r.Request.URL.String()]
		mu.Unlock()
		if cnt == 0 {
			emptyStreak++
		} else {
			emptyStreak = 0
		}
		pagesVisited++
		// условия остановки пагинации
		if o.MaxEmptyListingPages > 0 && emptyStreak >= o.MaxEmptyListingPages {
			log.Info("stop on empty listing pages (will not schedule more)", zap.Int("streak", emptyStreak))
			atomic.StoreInt64(&shouldStop, 1)
			return
		}
		if atomic.LoadInt64(&shouldStop) == 1 {
			return
		}
		if o.Pages > 0 && pagesVisited >= max(1, o.Pages) {
			return // достигли план по страницам
		}
		// запустить следующую страницу
		next := currentPage + 1
		if next >= o.StartPage+max(1, o.Pages) {
			return
		}
		currentPage = next
		u := o.buildPageURL(currentPage, log)
		if err := c.Visit(u); err != nil {
			log.Warn("visit list err: ", zap.Error(err))
		}
	})

	c.OnRequest(func(r *colly.Request) {
		// базовые заголовки
		r.Headers.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	})

	c.OnError(func(r *colly.Response, err error) {
		msg := fmt.Sprintf("HTTP %d %s: %v", r.StatusCode, r.Request.URL, err)
		log.Info(msg)
	})

	// запускам обход: стартуем с первой страницы, дальше листаем в OnScraped
	u := o.buildPageURL(currentPage, log)
	if err := c.Visit(u); err != nil {
		log.Warn("visit list err: ", zap.Error(err))
	}

	c.Wait()
	return nil
}

func snapshotPath(dir, pageURL string, body []byte) string {
	h := sha1.Sum(append([]byte(pageURL), body...))
	return filepath.Join(dir, fmt.Sprintf("%s_%s.html", safeName(pageURL), hex.EncodeToString(h[:8])))
}

func safeName(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.ReplaceAll(u, "/", "_")
	return u
}

func clampDuration(v, min, max time.Duration) time.Duration {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
