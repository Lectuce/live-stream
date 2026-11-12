package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVideoProbe(t *testing.T) {
	valid := `{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"12.5"},"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":1920,"height":1080},{"codec_type":"audio","codec_name":"aac"}]}`
	ms, err := validateProbe([]byte(valid))
	if err != nil || ms != 12500 {
		t.Fatalf("duration=%d error=%v", ms, err)
	}
	for name, payload := range map[string]string{
		"wrong codec":          strings.Replace(valid, "h264", "hevc", 1),
		"wrong pixel format":   strings.Replace(valid, "yuv420p", "yuv444p", 1),
		"oversized dimensions": strings.Replace(valid, "1920", "3840", 1),
		"invalid duration":     strings.Replace(valid, "12.5", "NaN", 1),
		"too long":             strings.Replace(valid, "12.5", "3601", 1),
		"wrong container":      strings.Replace(valid, "mov,mp4,m4a,3gp,3g2,mj2", "matroska", 1),
		"no video":             `{"format":{"format_name":"mp4","duration":"12"},"streams":[]}`,
		"bad json":             "broken",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateProbe([]byte(payload)); !errors.Is(err, ErrUnsupportedVideo) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
