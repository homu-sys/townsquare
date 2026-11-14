package web

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
)

const (
	maxAvatarBytes = 2 << 20
	avatarSize     = 128
)

func (s *Server) settingsAvatarPost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxAvatarBytes + 1<<20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Could not read that image. It may be too large.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !s.checkCSRF(w, r) {
		return
	}
	switch r.PostFormValue("action") {
	case "remove":
		if err := s.store.SetAvatar(u.ID, ""); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not remove your photo.")
			return
		}
		s.setFlash(w, "Profile photo removed.")
		redirect(w, r, "/settings")
		return
	case "upload", "":
	default:
		redirect(w, r, "/settings")
		return
	}

	file, _, err := r.FormFile("avatar")
	if err != nil {
		s.setFlash(w, "Choose a PNG or JPEG image to upload.")
		redirect(w, r, "/settings")
		return
	}
	defer file.Close()
	avatarImg, err := readAvatar(file)
	if err != nil {
		s.setFlash(w, err.Error())
		redirect(w, r, "/settings")
		return
	}
	if s.avatarChecker != nil {
		verdict, err := s.avatarChecker.Check(avatarImg)
		if err != nil {
			log.Printf("avatar moderation: %v", err)
			s.setFlash(w, "Could not check that image right now. Please try again later.")
			redirect(w, r, "/settings")
			return
		}
		if msg := verdict.Rejection(s.cfg.ImageNSFWThreshold, s.cfg.ImageGoreThreshold); msg != "" {
			s.setFlash(w, msg)
			redirect(w, r, "/settings")
			return
		}
	}
	avatar, err := encodeAvatar(resizeAvatar(avatarImg, avatarSize))
	if err != nil {
		s.setFlash(w, err.Error())
		redirect(w, r, "/settings")
		return
	}
	if err := s.store.SetAvatar(u.ID, avatar); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save your photo.")
		return
	}
	s.setFlash(w, "Profile photo updated.")
	redirect(w, r, "/settings")
}

func readAvatar(r io.Reader) (*image.RGBA, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAvatarBytes+1))
	if err != nil {
		return nil, errors.New("Could not read that image.")
	}
	if len(data) > maxAvatarBytes {
		return nil, errors.New("Profile photos must be 2 MB or smaller.")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, errors.New("Use a PNG or JPEG image.")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return nil, errors.New("That image has unsupported dimensions.")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Could not read that image.")
	}

	b := img.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	if size < 1 {
		return nil, errors.New("Could not read that image.")
	}
	square := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(square, square.Bounds(), img,
		image.Pt(b.Min.X+(b.Dx()-size)/2, b.Min.Y+(b.Dy()-size)/2), draw.Src)
	return square, nil
}

func encodeAvatar(scaled *image.RGBA) (string, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, scaled); err != nil {
		return "", errors.New("Could not convert that image.")
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func resizeAvatar(src *image.RGBA, size int) *image.RGBA {
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		sy0 := sb.Min.Y + y*sb.Dy()/size
		sy1 := sb.Min.Y + (y+1)*sb.Dy()/size
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < size; x++ {
			sx0 := sb.Min.X + x*sb.Dx()/size
			sx1 := sb.Min.X + (x+1)*sb.Dx()/size
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, b, a uint64
			var n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					c := src.RGBAAt(sx, sy)
					r += uint64(c.R)
					g += uint64(c.G)
					b += uint64(c.B)
					a += uint64(c.A)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r / n),
				G: uint8(g / n),
				B: uint8(b / n),
				A: uint8(a / n),
			})
		}
	}
	return dst
}
