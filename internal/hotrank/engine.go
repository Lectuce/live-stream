package hotrank

import (
	"context"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/repo"
	"log"
	"math"
	"os"
	"time"

	"github.com/google/uuid"
)

type OnlineSnapshotter interface {
	SnapshotOnlineCounts() map[uint64]int
}

type Settings struct {
	RefreshInterval time.Duration
	PresenceTTL     time.Duration
	OnlineWeight    float64
	LikeWeight      float64
	DanmaWeight     float64
	FreshnessWeight float64
	FreshnessDecay  time.Duration
}

type Signals struct {
	Online    int64
	Likes     float64
	Danmas    float64
	StartedAt time.Time
}

type Engine struct {
	snapshotter OnlineSnapshotter
	settings    Settings
	instanceID  string
	lastOnline  map[uint64]int
}

func New(snapshotter OnlineSnapshotter) *Engine {
	hostname, _ := os.Hostname()
	return &Engine{
		snapshotter: snapshotter,
		settings:    loadSettings(),
		instanceID:  fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString()),
		lastOnline:  make(map[uint64]int),
	}
}

func (engine *Engine) Run(ctx context.Context) {
	if engine.snapshotter == nil {
		log.Print("hot ranking disabled: online snapshotter is nil")
		return
	}
	if err := engine.refresh(ctx, time.Now()); err != nil {
		log.Printf("refresh hot room ranking failed: %v", err)
	}
	ticker := time.NewTicker(engine.settings.RefreshInterval)
	defer ticker.Stop()
	defer engine.removePresence()
	log.Printf("hot ranking started, interval=%s, instance=%s", engine.settings.RefreshInterval, engine.instanceID)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := engine.refresh(ctx, now); err != nil {
				log.Printf("refresh hot room ranking failed: %v", err)
			}
		}
	}
}

func (engine *Engine) refresh(ctx context.Context, now time.Time) error {
	localOnline := engine.snapshotter.SnapshotOnlineCounts()
	if err := dao.ReportLocalOnlineCounts(
		ctx,
		engine.instanceID,
		localOnline,
		engine.lastOnline,
		engine.settings.PresenceTTL,
		now,
	); err != nil {
		return fmt.Errorf("report local online counts: %w", err)
	}
	engine.lastOnline = localOnline

	rooms, err := repo.SelectActiveRoomHeatSources(ctx)
	if err != nil {
		return fmt.Errorf("list active rooms: %w", err)
	}
	roomIDs := make([]uint64, 0, len(rooms))
	for _, room := range rooms {
		roomIDs = append(roomIDs, room.RoomID)
	}
	online, err := dao.ReadGlobalOnlineCounts(ctx, roomIDs, now.Add(-engine.settings.PresenceTTL))
	if err != nil {
		return fmt.Errorf("read global online counts: %w", err)
	}
	activity, err := dao.ReadRecentRoomActivity(ctx, roomIDs, now)
	if err != nil {
		return fmt.Errorf("read recent room activity: %w", err)
	}

	scores := make(map[uint64]float64, len(rooms))
	for _, room := range rooms {
		startedAt := now
		if room.StartAt != nil {
			startedAt = *room.StartAt
		}
		signals := activity[room.RoomID]
		scores[room.RoomID] = CalculateHotScore(engine.settings, Signals{
			Online:    online[room.RoomID],
			Likes:     signals.Likes,
			Danmas:    signals.Danmas,
			StartedAt: startedAt,
		}, now)
	}
	if err := dao.ReplaceHotRoomRanking(ctx, scores); err != nil {
		return fmt.Errorf("replace Redis hot ranking: %w", err)
	}
	return nil
}

func (engine *Engine) removePresence() {
	if len(engine.lastOnline) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := dao.ReportLocalOnlineCounts(
		ctx,
		engine.instanceID,
		map[uint64]int{},
		engine.lastOnline,
		engine.settings.PresenceTTL,
		time.Now(),
	); err != nil {
		log.Printf("remove hot ranking presence failed: %v", err)
	}
}

func CalculateHotScore(settings Settings, signals Signals, now time.Time) float64 {
	age := now.Sub(signals.StartedAt)
	if age < 0 {
		age = 0
	}
	decay := settings.FreshnessDecay
	if decay <= 0 {
		decay = 30 * time.Minute
	}
	freshness := math.Exp(-float64(age) / float64(decay))
	return settings.OnlineWeight*math.Log1p(math.Max(0, float64(signals.Online))) +
		settings.LikeWeight*math.Log1p(math.Max(0, signals.Likes)) +
		settings.DanmaWeight*math.Log1p(math.Max(0, signals.Danmas)) +
		settings.FreshnessWeight*freshness
}

func loadSettings() Settings {
	defaults := Settings{
		RefreshInterval: 3 * time.Second,
		PresenceTTL:     10 * time.Second,
		OnlineWeight:    3,
		LikeWeight:      2,
		DanmaWeight:     1,
		FreshnessWeight: 1,
		FreshnessDecay:  30 * time.Minute,
	}
	if config.GlobalConfig == nil {
		return defaults
	}
	cfg := config.GlobalConfig.Danmaku.HotRanking
	if cfg.RefreshIntervalMilliseconds > 0 {
		defaults.RefreshInterval = time.Duration(cfg.RefreshIntervalMilliseconds) * time.Millisecond
	}
	if cfg.PresenceTTLSeconds > 0 {
		defaults.PresenceTTL = time.Duration(cfg.PresenceTTLSeconds) * time.Second
	}
	defaults.OnlineWeight = configuredWeight(cfg.OnlineWeight, defaults.OnlineWeight)
	defaults.LikeWeight = configuredWeight(cfg.LikeWeight, defaults.LikeWeight)
	defaults.DanmaWeight = configuredWeight(cfg.DanmaWeight, defaults.DanmaWeight)
	defaults.FreshnessWeight = configuredWeight(cfg.FreshnessWeight, defaults.FreshnessWeight)
	if cfg.FreshnessDecayMinutes > 0 {
		defaults.FreshnessDecay = time.Duration(cfg.FreshnessDecayMinutes * float64(time.Minute))
	}
	return defaults
}

func configuredWeight(value float64, fallback float64) float64 {
	if value < 0 {
		return fallback
	}
	return value
}
