package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
)

type LiveMedia struct{ Secret, PushBase, PlayBase, API string }

func LiveMediaConfig() (LiveMedia, error) {
	c := LiveMedia{os.Getenv("LIVE_MEDIA_SECRET"), strings.TrimRight(os.Getenv("LIVE_PUSH_BASE"), "/"), strings.TrimRight(os.Getenv("LIVE_PLAY_BASE"), "/"), strings.TrimRight(os.Getenv("LIVE_SRS_API"), "/")}
	if c.API == "" {
		c.API = "http://127.0.0.1:1985"
	}
	if len(c.Secret) < 32 {
		return c, errors.New("LIVE_MEDIA_SECRET must contain at least 32 characters")
	}
	push, e := url.Parse(c.PushBase)
	if e != nil || push.Scheme != "rtmp" || push.Host == "" || push.Path != "/live" || push.RawQuery != "" {
		return c, errors.New("LIVE_PUSH_BASE must be rtmp://host[:port]/live")
	}
	play, e := url.Parse(c.PlayBase)
	if e != nil || !((play.Scheme == "http" || play.Scheme == "https") && play.Host != "" || strings.HasPrefix(c.PlayBase, "/") && !strings.HasPrefix(c.PlayBase, "//")) || play.RawQuery != "" {
		return c, errors.New("LIVE_PLAY_BASE must be an HTTP URL or same-origin path")
	}
	api, e := url.Parse(c.API)
	if e != nil || api.Scheme != "http" || api.Host == "" {
		return c, errors.New("LIVE_SRS_API must be a private HTTP URL")
	}
	return c, nil
}

func (c LiveMedia) Token(stream string) string {
	m := hmac.New(sha256.New, []byte(c.Secret))
	m.Write([]byte("publish:" + stream))
	return hex.EncodeToString(m.Sum(nil))
}
