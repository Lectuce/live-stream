package service

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LiveCredentials struct {
	*model.LiveSession
	Server    string `json:"server"`
	StreamKey string `json:"stream_key"`
	PlayURL   string `json:"play_url"`
}

func PrepareLive(ctx context.Context, room, owner uint64, title string) (*LiveCredentials, error) {
	cfg, err := config.LiveMediaConfig()
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) == 0 || len([]rune(title)) > 100 {
		return nil, errors.New("直播标题需要1–100字")
	}
	s, err := repo.PrepareLive(ctx, room, owner, title)
	if err != nil {
		return nil, err
	}
	return &LiveCredentials{s, cfg.PushBase, s.Stream + "?token=" + cfg.Token(s.Stream), cfg.PlayBase + "/" + s.Stream + ".m3u8"}, nil
}

type MediaHook struct {
	Action   string `json:"action"`
	App      string `json:"app"`
	Stream   string `json:"stream"`
	ClientID string `json:"client_id"`
	Param    string `json:"param"`
}

func ValidMediaHook(cfg config.LiveMedia, h MediaHook) bool {
	if h.App != "live" || h.ClientID == "" || len(h.ClientID) > 100 || len(h.Stream) != 36 {
		return false
	}
	if h.Action != "on_publish" && h.Action != "on_unpublish" {
		return false
	}
	p, err := url.ParseQuery(strings.TrimPrefix(h.Param, "?"))
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(p.Get("token")), []byte(cfg.Token(h.Stream)))
}

// The callback listener is loopback-only and must never be reverse proxied publicly.
func MediaHookHandler(cfg config.LiveMedia) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.URL.Path != "/hooks/srs" {
			http.Error(w, `{"code":1}`, 404)
			return
		}
		var h MediaHook
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&h) != nil || !ValidMediaHook(cfg, h) {
			http.Error(w, `{"code":1}`, 403)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		s, err := repo.ApplyLiveCallback(ctx, h.Stream, h.ClientID, h.Action == "on_publish", cfg.PlayBase+"/"+h.Stream+".m3u8")
		if err != nil {
			http.Error(w, `{"code":1}`, 409)
			return
		}
		if s.Status == "ended" {
			notifyLiveEnded(ctx, s.RoomID)
		}
		_, _ = io.WriteString(w, `{"code":0}`)
	})
}

func notifyLiveEnded(ctx context.Context, room uint64) {
	payload, _ := json.Marshal(model.Message{Type: model.MsgTypeSystem, RoomID: room, Data: map[string]any{"sub_type": "room_status", "status": 0, "reason": "stream_ended"}})
	if err := dao.Publish(ctx, dao.DanmuChannel, string(payload)); err != nil {
		log.Printf("live status notification failed room=%d: %v", room, err)
	}
}

var mediaHTTP = &http.Client{Timeout: 4 * time.Second}

type srsStream struct {
	Name    string `json:"name"`
	App     string `json:"app"`
	Publish struct {
		Active bool   `json:"active"`
		CID    string `json:"cid"`
	} `json:"publish"`
}

func mediaRequest(ctx context.Context, cfg config.LiveMedia, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, cfg.API+path, nil)
	if err != nil {
		return err
	}
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("SRS status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func kickPublisher(ctx context.Context, cfg config.LiveMedia, client string) error {
	if client == "" {
		return nil
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := mediaRequest(ctx, cfg, "DELETE", "/api/v1/clients/"+url.PathEscape(client), &result); err != nil {
		return err
	}
	if result.Code != 0 {
		return fmt.Errorf("SRS kick code %d", result.Code)
	}
	return nil
}

func StopMediaLive(ctx context.Context, room, owner uint64) error {
	s, err := repo.EndLive(ctx, room, owner)
	if err != nil {
		return err
	}
	notifyLiveEnded(ctx, room)
	if cfg, e := config.LiveMediaConfig(); e == nil && s.ClientID != "" {
		// Revocation is durable first. Reconciliation retries a failed disconnect.
		if e = kickPublisher(ctx, cfg, s.ClientID); e != nil {
			log.Printf("media disconnect deferred session=%d", s.ID)
		}
	}
	return nil
}

func ReconcileLive(ctx context.Context, cfg config.LiveMedia) error {
	if err := dao.SC.Db.WithContext(ctx).Model(&model.LiveSession{}).Where("status = ? AND expires_at <= ?", "pending", time.Now()).Updates(map[string]any{"status": "ended", "ended_at": time.Now()}).Error; err != nil {
		return err
	}
	var response struct {
		Code    int         `json:"code"`
		Streams []srsStream `json:"streams"`
	}
	// Fail closed on a truncated list: never end rooms from incomplete evidence.
	if err := mediaRequest(ctx, cfg, "GET", "/api/v1/streams?start=0&count=1000", &response); err != nil {
		return err
	}
	if response.Code != 0 || response.Streams == nil || len(response.Streams) >= 1000 {
		return errors.New("incomplete SRS stream inventory")
	}
	current := make(map[string]string)
	for _, stream := range response.Streams {
		if stream.App == "live" && stream.Publish.Active {
			current[stream.Name] = stream.Publish.CID
		}
	}
	var sessions []model.LiveSession
	if err := dao.SC.Db.WithContext(ctx).Where("status = ?", "live").Find(&sessions).Error; err != nil {
		return err
	}
	for _, s := range sessions {
		if current[s.Stream] == s.ClientID {
			continue
		}
		// on_publish precedes SRS stream registration. Give it time to appear.
		if s.StartedAt == nil || time.Since(*s.StartedAt) < time.Minute {
			continue
		}
		if ended, err := repo.ApplyLiveCallback(ctx, s.Stream, s.ClientID, false, ""); err != nil {
			return err
		} else if ended.Status == "ended" {
			notifyLiveEnded(ctx, s.RoomID)
		}
	}
	// An owner may end while SRS is unavailable. Revoke leftover publishers on recovery.
	for stream, client := range current {
		var s model.LiveSession
		if err := dao.SC.Db.WithContext(ctx).Where("stream = ?", stream).First(&s).Error; err != nil {
			return err
		}
		if s.Status == "ended" {
			if err := kickPublisher(ctx, cfg, client); err != nil {
				return err
			}
		}
	}
	return nil
}

func StartLiveMedia(ctx context.Context) (<-chan struct{}, error) {
	done := make(chan struct{})
	cfg, err := config.LiveMediaConfig()
	if err != nil {
		close(done)
		return done, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:9001")
	if err != nil {
		close(done)
		return done, err
	}
	server := &http.Server{Handler: MediaHookHandler(cfg), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("media callback listener: %v", err)
		}
	}()
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(stop)
				return
			case <-ticker.C:
				check, cancel := context.WithTimeout(ctx, 15*time.Second)
				if err := ReconcileLive(check, cfg); err != nil {
					log.Printf("live media reconciliation: %v", err)
				}
				cancel()
			}
		}
	}()
	return done, nil
}
