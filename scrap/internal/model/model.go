package model

import (
	"time"

	"gorm.io/datatypes"
)

// ---------- listing_raw: сырые записи с площадок ----------
type ListingRaw struct {
	ID               uint64 `gorm:"primaryKey"`
	Source           string `gorm:"not null;index;uniqueIndex:uidx_source_deal_external_id"`
	DealType         string `gorm:"not null;index;uniqueIndex:uidx_source_deal_external_id"` // sale/rent_long/rent_daily
	ExternalID       string `gorm:"not null;uniqueIndex:uidx_source_deal_external_id"`
	URL              string
	Payload          datatypes.JSON `gorm:"type:jsonb;not null"`
	Title            string
	Description      string
	PriceValue       *float64 `gorm:"type:numeric(14,2)"`
	PriceCurrency    string
	PricePeriod      *string // "month" | "day" | nil
	Rooms            *int
	AreaTotal        *float64
	AreaLiving       *float64
	AreaKitchen      *float64
	Floor            *int
	FloorsTotal      *int
	YearBuilt        *int
	HouseMaterial    string
	Condition        string
	AddressText      string
	Lat              *float64
	Lon              *float64
	District         string
	Metro            string
	MetroStation     *string // нормализованное название станции (сырые)
	MetroWalkMin     *int
	ContactPhoneHash string
	CollectedAt      time.Time `gorm:"not null;default:now()"`
	CreatedAt        time.Time `gorm:"not null;default:now()"`
	UpdatedAt        time.Time `gorm:"not null;default:now()"`
}

func (ListingRaw) TableName() string { return "listing_raw" }

// ---------- listing: нормализованная карточка ----------
type Listing struct {
	ID               uint64 `gorm:"primaryKey"`
	Source           string `gorm:"not null;index;uniqueIndex:uidx_listing_source_deal_external_id"`
	DealType         string `gorm:"not null;index;uniqueIndex:uidx_listing_source_deal_external_id"`
	ExternalID       string `gorm:"not null;uniqueIndex:uidx_listing_source_deal_external_id"`
	URL              string
	Title            string
	Description      string
	PriceRUB         *float64 `gorm:"type:numeric(14,2)"`
	PricePeriod      *string  // "month" | "day" | nil
	PricePerM2       *float64 `gorm:"column:price_per_m2;type:numeric(14,2);index:idx_listing_ppm"`
	Rooms            *int
	AreaTotal        *float64
	AreaLiving       *float64
	AreaKitchen      *float64
	Floor            *int
	FloorsTotal      *int
	YearBuilt        *int
	HouseMaterial    string
	Condition        string
	AddressNorm      string
	City             string `gorm:"index:idx_city_district,priority:1"`
	District         string `gorm:"index:idx_city_district,priority:2"`
	Metro            string
	MetroStation     string
	MetroWalkMin     *int
	ContactPhoneHash string
	IsActive         bool `gorm:"default:true"`
	FirstSeen        *time.Time
	LastSeen         *time.Time
	Lat              *float64
	Lon              *float64
	CreatedAt        time.Time `gorm:"not null;default:now()"`
	UpdatedAt        time.Time `gorm:"not null;default:now()"`
}

func (Listing) TableName() string { return "listing" }

// ---------- geocode_cache: кэш результатов геокодинга ----------
type GeocodeCache struct {
	AddressHash string `gorm:"primaryKey"`
	AddressNorm string
	Lat         *float64
	Lon         *float64
	Quality     string
	Provider    string
	CreatedAt   time.Time `gorm:"not null;default:now()"`
}

func (GeocodeCache) TableName() string { return "geocode_cache" }

// ---------- price_history: история цен по listing ----------
type PriceHistory struct {
	ID        uint64    `gorm:"primaryKey"`
	ListingID uint64    `gorm:"not null;index"`
	TS        time.Time `gorm:"not null;default:now()"`
	PriceRUB  float64   `gorm:"type:numeric(14,2);not null"`
}

func (PriceHistory) TableName() string { return "price_history" }
