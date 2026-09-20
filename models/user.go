package models

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

type User struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Nama      string
	Username  string    `gorm:"uniqueIndex"`
	Password  string    `json:"-"`
	Role      string
	NoHP      string
	Email     string
	IsActive  bool      `gorm:"default:true"`
	CreatedAt time.Time
}

func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	u.ID = uuid.New()
	u.IsActive = true
	return nil
}