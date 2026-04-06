package idgen

import (
	"fmt"
	"strings"
	"sync/atomic"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const (
	DefaultLength  = 12
	ParanoidLength = 16
)

var currentLength atomic.Int32

func init() {
	currentLength.Store(DefaultLength)
}

func ConfigureLength(length int) {
	if length != ParanoidLength {
		length = DefaultLength
	}
	currentLength.Store(int32(length))
}

func NewID(prefix string) (string, error) {
	clean := strings.TrimSpace(prefix)
	if clean == "" {
		return "", fmt.Errorf("id prefix is required")
	}
	id, err := gonanoid.New(int(currentLength.Load()))
	if err != nil {
		return "", err
	}
	return clean + "_" + id, nil
}
