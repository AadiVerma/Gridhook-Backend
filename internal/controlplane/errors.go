package controlplane

import "errors"

var (
	ErrNotFound = errors.New("controlplane: not found")

	ErrValidation = errors.New("controlplane: validation failed")

	ErrUnauthorized = errors.New("controlplane: unauthorized")

	ErrConflict = errors.New("controlplane: conflict")
)
