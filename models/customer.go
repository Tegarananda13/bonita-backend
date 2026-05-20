package models

import (
	"time"

	"github.com/google/uuid"
)

type Customer struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Nama      string
	NoHP      string
	Email     string
	Alamat    string
	CreatedAt time.Time
}