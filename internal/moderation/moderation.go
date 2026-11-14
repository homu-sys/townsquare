package moderation

import (
	"errors"
	"image"
)

const InputSize = 224

var ErrUnavailable = errors.New("image moderation is unavailable on this platform")

type Checker interface {
	Check(image.Image) (Verdict, error)
}

type Verdict struct {
	NSFL float64
	NSFW float64
	SFW  float64
}

func (v Verdict) Rejection(nsfwThreshold, goreThreshold float64) string {
	if v.NSFL >= goreThreshold {
		return "That image appears to contain graphic violence. Please choose a different photo."
	}
	if v.NSFW >= nsfwThreshold {
		return "That image appears to contain adult content. Please choose a different photo."
	}
	return ""
}
