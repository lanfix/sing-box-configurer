package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/settings"
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

// TestAutoCheck проверяет расписание автоматической проверки: сразу после включения, затем через интервал,
// после ошибки — через autoCheckRetry. Пока проверка включена, кэш держится весь интервал.
func TestAutoCheck(t *testing.T) {
	updates := &fakeUpdates{
		err:   nil,
		calls: 0,
	}

	service := NewService(platform.Platform{
		Name:    platform.NameDocker,
		SingBox: nil,
		Updates: updates,
	})

	config := settings.Updates{
		AutoCheck:     false,
		IntervalHours: 6,
	}

	service.StartAutoCheck(t.Context(), func() settings.Updates {
		return config
	})

	if result := service.autoCheckTick(context.Background()); result != nil || updates.calls != 0 {
		t.Fatalf("disabled auto check must not run: %+v, calls = %d", result, updates.calls)
	}

	config.AutoCheck = true

	result := service.autoCheckTick(context.Background())
	if result == nil || updates.calls != 1 {
		t.Fatalf("first auto check must run immediately, calls = %d", updates.calls)
	}

	if !result.AutoCheck || result.NextCheckAt == nil || result.NextCheckAt.Sub(result.CheckedAt) != 6*time.Hour {
		t.Errorf("result schedule = %v %v", result.AutoCheck, result.NextCheckAt)
	}

	if service.autoCheckTick(context.Background()) != nil || updates.calls != 1 {
		t.Fatalf("auto check must wait for interval, calls = %d", updates.calls)
	}

	// Проверка без force берет результат из кэша, пока не прошел интервал, хотя час уже прошел.
	service.lastCheck.CheckedAt = time.Now().Add(-2 * time.Hour)
	service.Check(context.Background(), false)

	if updates.calls != 1 {
		t.Fatalf("result must be cached for the interval, calls = %d", updates.calls)
	}

	// Ошибка повторяется через autoCheckRetry, а не через интервал.
	service.lastCheck.CheckedAt = time.Now().Add(-6 * time.Hour)
	updates.err = errors.New("timeout")

	if result := service.autoCheckTick(context.Background()); result == nil || result.Error == "" || updates.calls != 2 {
		t.Fatalf("auto check must run after interval, calls = %d", updates.calls)
	}

	service.lastCheck.CheckedAt = time.Now().Add(-autoCheckRetry - time.Second)

	if service.autoCheckTick(context.Background()) == nil || updates.calls != 3 {
		t.Fatalf("failed auto check must be retried, calls = %d", updates.calls)
	}
}
