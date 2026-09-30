package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
)

// fakeUpdates — платформа, у которой можно менять ответ списка релизов.
type fakeUpdates struct {
	err   error
	calls int
}

func (f *fakeUpdates) Releases(_ context.Context) ([]platform.Release, error) {
	f.calls++

	return []platform.Release{}, f.err
}

func (f *fakeUpdates) Changelog(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeUpdates) Unsupported(_ context.Context) string {
	return ""
}

func (f *fakeUpdates) Start(_ context.Context, _, _ string) (time.Time, error) {
	return time.Time{}, nil
}

func (f *fakeUpdates) Job(_ context.Context) (platform.Job, error) {
	return platform.Job{}, nil
}

// TestCheckErrorIsNotCachedForAnHour проверяет, что неудачная проверка повторяется через минуту, а успешная кэшируется.
func TestCheckErrorIsNotCachedForAnHour(t *testing.T) {
	updates := &fakeUpdates{
		err:   errors.New("lookup hub.docker.com: server misbehaving"),
		calls: 0,
	}

	service := NewService(platform.Platform{
		Name:    platform.NameDocker,
		SingBox: nil,
		Updates: updates,
	})

	if result := service.Check(context.Background(), false); result.Error == "" {
		t.Fatal("expected error")
	}

	// Сразу после ошибки — кэш, через минуту — новая проверка.
	service.Check(context.Background(), false)

	if updates.calls != 1 {
		t.Fatalf("calls = %d, want cached error", updates.calls)
	}

	service.lastCheck.CheckedAt = time.Now().Add(-checkErrorCacheTTL - time.Second)
	updates.err = nil

	if result := service.Check(context.Background(), false); result.Error != "" || updates.calls != 2 {
		t.Fatalf("error must be rechecked after a minute: %+v, calls = %d", result, updates.calls)
	}

	// Успешная проверка держится в кэше дольше минуты.
	service.lastCheck.CheckedAt = time.Now().Add(-checkErrorCacheTTL - time.Second)
	service.Check(context.Background(), false)

	if updates.calls != 2 {
		t.Errorf("successful check must be cached, calls = %d", updates.calls)
	}
}
