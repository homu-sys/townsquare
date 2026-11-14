//go:build !linux || !amd64

package moderation

func New() (Checker, error) {
	return nil, ErrUnavailable
}
