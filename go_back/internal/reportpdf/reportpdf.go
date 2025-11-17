package reportpdf

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"

	"github.com/jung-kurt/gofpdf"

	"go_back/internal/mlclient"
)

type Options struct {
	// на будущее: логотип, тема, локаль и т.п.
	FontDir string
}

// helper: базовая инициализация PDF с UTF-8 шрифтом
func newPDFWithFont(opts Options) (*gofpdf.Fpdf, error) {
	fontDir := opts.FontDir
	if fontDir == "" {
		fontDir = "internal/reportpdf/fonts"
	}

	pdf := gofpdf.New("P", "mm", "A4", "")

	// Регистрируем базовый стиль (regular)
	fontPathRegular := filepath.Join(fontDir, "timesnewromanpsmt.ttf")
	pdf.AddUTF8Font("timesnewromanpsmt", "", fontPathRegular)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("add regular font: %w", err)
	}

	// Регистрируем жирный стиль (bold)
	fontPathBold := filepath.Join(fontDir, "times-new-roman-bold.ttf") // <-- ИСПРАВЛЕНО
	pdf.AddUTF8Font("timesnewromanpsmt", "B", fontPathBold)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("add bold font: %w", err)
	}

	pdf.SetTitle("Отчет об оценке аренды", false)
	pdf.SetAuthor("RealVal / RentAdvisor", false)

	pdf.AddPage()
	pdf.SetFont("timesnewromanpsmt", "", 16)

	return pdf, nil
}

// RenderReportPDF генерирует PDF по полному отчету.
func RenderReportPDF(r *mlclient.Report, opts Options) ([]byte, error) {
	pdf, err := newPDFWithFont(opts)
	if err != nil {
		return nil, err
	}

	// ---- Заголовок ----
	title := "Отчет об оценке арендной стоимости"
	pdf.CellFormat(190, 10, title, "", 1, "C", false, 0, "")

	pdf.Ln(2)
	pdf.SetFont("timesnewromanpsmt", "", 11)
	addrLine := fmt.Sprintf("%s, %s", r.Object.City, r.Object.Address)
	pdf.MultiCell(190, 6, addrLine, "", "C", false)

	pdf.Ln(4)

	// ---- 1. Итоговая оценка ----
	pdf.SetFont("timesnewromanpsmt", "B", 13)
	pdf.CellFormat(190, 8, "1. Итоговая оценка аренды", "", 1, "", false, 0, "")
	pdf.SetFont("timesnewromanpsmt", "", 11)

	mainText := fmt.Sprintf(
		"Оценочная ставка долгосрочной аренды: %.0f ₽ в месяц.\n"+
			"Ожидаемый диапазон: от %.0f до %.0f ₽ в месяц.\n"+
			"Валюта: %s, тип сделки: %s.",
		r.Pricing.PredictionRub,
		r.Pricing.IntervalLowRub,
		r.Pricing.IntervalHighRub,
		r.Pricing.Currency,
		r.Pricing.DealType,
	)
	pdf.MultiCell(190, 6, mainText, "", "", false)
	pdf.Ln(3)

	// ---- 2. Характеристики объекта ----
	pdf.SetFont("timesnewromanpsmt", "B", 13)
	pdf.CellFormat(190, 8, "2. Характеристики объекта", "", 1, "", false, 0, "")
	pdf.SetFont("timesnewromanpsmt", "", 11)

	objLines := []string{
		fmt.Sprintf("Город: %s", r.Object.City),
		fmt.Sprintf("Адрес: %s", r.Object.Address),
	}

	// Определяем тип жилья
	if r.Object.Rooms == 0 {
		objLines = append(objLines, "Тип: студия")
	} else {
		objLines = append(objLines, fmt.Sprintf("Комнат: %d", r.Object.Rooms))
	}

	objLines = append(objLines, fmt.Sprintf("Общая площадь: %.1f м²", r.Object.AreaTotal))

	if r.Object.Floor != nil && r.Object.FloorsTotal != nil {
		objLines = append(objLines, fmt.Sprintf("Этаж: %d из %d", *r.Object.Floor, *r.Object.FloorsTotal))
	}
	if r.Object.YearBuilt != nil {
		objLines = append(objLines, fmt.Sprintf("Год постройки: %d", *r.Object.YearBuilt))
	}
	if r.Object.HouseMaterial != nil && *r.Object.HouseMaterial != "" {
		objLines = append(objLines, fmt.Sprintf("Материал дома: %s", *r.Object.HouseMaterial))
	}
	if r.Object.Condition != nil && *r.Object.Condition != "" {
		objLines = append(objLines, fmt.Sprintf("Состояние: %s", *r.Object.Condition))
	}

	for _, line := range objLines {
		pdf.CellFormat(190, 6, line, "", 1, "", false, 0, "")
	}
	pdf.Ln(3)

	// ---- 3. Геоположение и окружение ----
	pdf.SetFont("timesnewromanpsmt", "B", 13) // <-- ИЗМЕНЕНО
	pdf.CellFormat(190, 8, "3. Геоположение и окружение", "", 1, "", false, 0, "")
	pdf.SetFont("timesnewromanpsmt", "", 11) // <-- ИЗМЕНЕНО

	if r.Enriched.DistToCenterKm != nil {
		pdf.CellFormat(190, 6,
			fmt.Sprintf("Расстояние до центра города: %.1f км", *r.Enriched.DistToCenterKm),
			"", 1, "", false, 0, "")
	}
	if r.Enriched.MetroStation != nil && *r.Enriched.MetroStation != "" {
		line := fmt.Sprintf("Ближайшее метро: %s", *r.Enriched.MetroStation)
		if r.Enriched.MetroWalkMin != nil {
			mins := int(math.Round(*r.Enriched.MetroWalkMin))
			line += fmt.Sprintf(", ~%d минут пешком", mins)
		}
		pdf.CellFormat(190, 6, line, "", 1, "", false, 0, "")
	}
	if r.Enriched.Density500m != nil {
		pdf.CellFormat(190, 6,
			fmt.Sprintf("Плотность предложений в радиусе 500 м (по выборке): %.0f", *r.Enriched.Density500m),
			"", 1, "", false, 0, "")
	}
	pdf.Ln(3)

	// ---- 4. Текстовое объяснение (LLM) ----
	if r.Text != nil {
		pdf.SetFont("timesnewromanpsmt", "B", 13) // <-- ИЗМЕНЕНО
		pdf.CellFormat(190, 8, "4. Объяснение оценки", "", 1, "", false, 0, "")
		pdf.SetFont("timesnewromanpsmt", "", 11) // <-- ИЗМЕНЕНО

		pdf.MultiCell(190, 5, r.Text.SummaryLong, "", "", false)
		pdf.Ln(2)

		if len(r.Text.FactorsSummary) > 0 {
			pdf.CellFormat(190, 6, "Ключевые факторы:", "", 1, "", false, 0, "")
			for _, f := range r.Text.FactorsSummary {
				pdf.CellFormat(5, 5, "•", "", 0, "", false, 0, "")
				pdf.CellFormat(185, 5, f, "", 1, "", false, 0, "")
			}
		}
		pdf.Ln(3)
	}

	// ---- 5. Сопоставимые объекты ----
	if len(r.Comparables) > 0 {
		pdf.SetFont("timesnewromanpsmt", "B", 13)
		pdf.CellFormat(190, 8, "5. Сопоставимые объекты", "", 1, "", false, 0, "")
		pdf.SetFont("timesnewromanpsmt", "", 10)

		// Заголовок таблицы
		pdf.SetFillColor(230, 230, 230)
		pdf.CellFormat(45, 7, "Цена, ₽", "1", 0, "C", true, 0, "")
		pdf.CellFormat(20, 7, "Комнат", "1", 0, "C", true, 0, "")
		pdf.CellFormat(30, 7, "Площадь, м²", "1", 0, "C", true, 0, "")
		pdf.CellFormat(35, 7, "Расстояние, км", "1", 0, "C", true, 0, "")
		pdf.CellFormat(60, 7, "Метро", "1", 1, "C", true, 0, "")

		pdf.SetFillColor(255, 255, 255)
		maxRows := 5
		for i, c := range r.Comparables {
			if i >= maxRows {
				break
			}
			price := fmt.Sprintf("%.0f", c.PriceRub)

			var rooms string
			if c.Rooms != nil {
				rooms = fmt.Sprintf("%d", *c.Rooms)
			}
			var area string
			if c.AreaTotal != nil {
				area = fmt.Sprintf("%.1f", *c.AreaTotal)
			}
			var dist string
			if c.DistanceKm != nil {
				dist = fmt.Sprintf("%.2f", *c.DistanceKm)
			}
			var metro string
			if c.MetroStation != nil {
				metro = *c.MetroStation
			}

			pdf.CellFormat(45, 6, price, "1", 0, "C", false, 0, "")
			pdf.CellFormat(20, 6, rooms, "1", 0, "C", false, 0, "")
			pdf.CellFormat(30, 6, area, "1", 0, "C", false, 0, "")
			pdf.CellFormat(35, 6, dist, "1", 0, "C", false, 0, "")
			pdf.CellFormat(60, 6, metro, "1", 1, "C", false, 0, "")

			// Добавляем ссылку на объявление, если есть
			if c.URL != nil && *c.URL != "" {
				pdf.SetFont("timesnewromanpsmt", "", 9)
				pdf.SetTextColor(0, 0, 255) // синий цвет для ссылки

				// Обрезаем длинную ссылку для отображения
				displayURL := *c.URL
				if len(displayURL) > 60 {
					displayURL = displayURL[:57] + "..."
				}

				// Добавляем кликабельную ссылку
				pdf.CellFormat(5, 5, "", "", 0, "", false, 0, "")
				pdf.WriteLinkString(5, displayURL, *c.URL)
				pdf.Ln(5)

				pdf.SetTextColor(0, 0, 0) // возвращаем черный цвет
				pdf.SetFont("timesnewromanpsmt", "", 10)
			}
		}
		pdf.Ln(3)
	}

	// ---- 6. Сведения о модели ----
	pdf.SetFont("timesnewromanpsmt", "B", 13) // <-- ИЗМЕНЕНО
	pdf.CellFormat(190, 8, "6. Сведения о модели", "", 1, "", false, 0, "")
	pdf.SetFont("timesnewromanpsmt", "", 11) // <-- ИЗМЕНЕНО

	metricsLine := fmt.Sprintf(
		"Модель: %s, целевая переменная: %s, лог-преобразование: %v",
		r.ModelInfo.ModelName, r.ModelInfo.Target, r.ModelInfo.LogTarget,
	)
	pdf.CellFormat(190, 6, metricsLine, "", 1, "", false, 0, "")

	if r.ModelInfo.ValidMAE != nil && r.ModelInfo.ValidRMSE != nil {
		pdf.CellFormat(190, 6,
			fmt.Sprintf("Качество на валидации: MAE=%.0f ₽, RMSE=%.0f ₽",
				*r.ModelInfo.ValidMAE, *r.ModelInfo.ValidRMSE),
			"", 1, "", false, 0, "")
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
