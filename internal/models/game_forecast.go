package models

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// GameForecast is the weather expected at kickoff.
//
// GameLiveState already carries weather, but only for games the scoreboard has
// covered, and the scoreboard covers the current week -- so there is nothing
// there for a game four days out, which is when a bet gets placed. This is the
// pre-game half; once a game starts the live strip is the single source and
// nothing here is rendered.
type GameForecast struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`

	// FetchedAt is how old the forecast is, which matters far more here than it
	// does for a rating: a rating moves once a week, a forecast moves all day.
	FetchedAt time.Time `gorm:"not null"`

	// Indoors is why this type has a Show method.
	//
	// A domed stadium does not return an empty forecast. It returns the weather
	// outside the roof, fully populated and entirely plausible -- all 70 indoor
	// games of the 2025 season carry a wind speed. Rendering one would state a
	// fact about the game that is false, which is worse than the blank the
	// reader would otherwise have got.
	Indoors bool `gorm:"not null;default:false"`

	Temperature   *decimal.Decimal `gorm:"type:decimal(5,1)"`
	Humidity      *int
	Precipitation *decimal.Decimal `gorm:"type:decimal(5,3)"`
	Snowfall      *decimal.Decimal `gorm:"type:decimal(5,3)"`
	WindSpeed     *decimal.Decimal `gorm:"type:decimal(5,1)"`

	// WeatherCondition is the text label, absent more often than not on a
	// forecast: 47 of 75 week-3 rows had none a week out against all 86 of the
	// current week's. WeatherConditionCode is 0 in exactly those cases, so it
	// is a presence flag rather than a second source of truth.
	WeatherCondition     *string `gorm:"type:varchar(64)"`
	WeatherConditionCode *int

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Game *Game `gorm:"foreignKey:GameID"`
}

// Show reports whether this forecast is worth drawing: an outdoor game with at
// least a temperature to report.
func (f *GameForecast) Show() bool {
	return f != nil && !f.Indoors && f.Temperature != nil
}

// TemperatureText renders the temperature in whole degrees Fahrenheit. Nobody
// bets on a tenth of a degree.
func (f *GameForecast) TemperatureText() string {
	if f == nil || f.Temperature == nil {
		return ""
	}
	return f.Temperature.Round(0).String() + "°F"
}

// WindText renders the wind in whole miles per hour, empty when there is no
// reading. Direction is not stored: without the stadium's orientation, "from
// the north-west" says nothing about which way a kick will drift.
func (f *GameForecast) WindText() string {
	if f == nil || f.WindSpeed == nil {
		return ""
	}
	return f.WindSpeed.Round(0).String() + " mph"
}

// HumidityText renders humidity as a percentage, empty when absent.
func (f *GameForecast) HumidityText() string {
	if f == nil || f.Humidity == nil {
		return ""
	}
	return strconv.Itoa(*f.Humidity) + "%"
}

// ConditionText is the provider's label for the sky, empty when it has not
// given one.
func (f *GameForecast) ConditionText() string {
	if f == nil || f.WeatherCondition == nil {
		return ""
	}
	return *f.WeatherCondition
}

// PrecipitationText renders expected rain or snow in inches, empty when both
// are absent or zero.
//
// Zero is treated as nothing to say rather than as "0.00 in": a forecast of no
// rain is the default state of a football game and does not need a row.
func (f *GameForecast) PrecipitationText() string {
	if f == nil {
		return ""
	}

	if f.Snowfall != nil && f.Snowfall.IsPositive() {
		return f.Snowfall.Round(2).String() + " in snow"
	}
	if f.Precipitation != nil && f.Precipitation.IsPositive() {
		return f.Precipitation.Round(2).String() + " in rain"
	}
	return ""
}

// BeforeCreate sets the UUID before creating a new forecast.
func (f *GameForecast) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}
