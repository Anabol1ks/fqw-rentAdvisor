package main

import (
	"fmt"

	"github.com/gocolly/colly"
)

func main() {

	// c := colly.NewCollector(colly.AllowedDomains("cian.ru"), colly.Async(true))
	// c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: 2, RandomDelay: 2 * time.Second})
	// c.OnHTML("article", func(e *colly.HTMLElement) {
	// 	// парсинг карточки, заполнение модели, сохранение в PG (RAW) и S3 снапшота
	// })
	// c.Visit("https://realty.yandex.ru/")
	// c.Wait()

	c := colly.NewCollector()

	// Find and visit all links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		e.Request.Visit(e.Attr("href"))
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL)
	})

	c.Visit("http://go-colly.org/")

}
