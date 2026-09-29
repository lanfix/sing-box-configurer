package outbound

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// ListURLTests возвращает копию списка urltest-ов.
func (m *Manager) ListURLTests() []URLTest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]URLTest, len(m.urlTests))

	for i, urlTest := range m.urlTests {
		result[i] = cloneURLTest(urlTest)
	}

	return result
}

// GetURLTest возвращает urltest с указанным ID.
func (m *Manager) GetURLTest(id string) (URLTest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	index := m.urlTestIndexLocked(id)
	if index < 0 {
		return URLTest{}, false
	}

	return cloneURLTest(m.urlTests[index]), true
}

// AddURLTest добавляет urltest.
func (m *Manager) AddURLTest(urlTest URLTest) (*URLTest, error) {
	urlTest.Normalize()

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.validateURLTestLocked(urlTest, ""); err != nil {
		return nil, err
	}

	urlTest.ID = uuid.NewString()
	urlTest.CreatedAt = time.Now()

	m.urlTests = append(m.urlTests, urlTest)

	if err := m.save(); err != nil {
		return nil, err
	}

	result := cloneURLTest(urlTest)

	return &result, nil
}

// UpdateURLTest заменяет параметры urltest-а с ID urlTest.ID. Тег не меняется: на него ссылаются группы.
func (m *Manager) UpdateURLTest(urlTest URLTest) error {
	urlTest.Normalize()

	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.urlTestIndexLocked(urlTest.ID)
	if index < 0 {
		return ErrURLTestNotFound
	}

	current := m.urlTests[index]

	urlTest.Tag = current.Tag
	urlTest.CreatedAt = current.CreatedAt

	if err := m.validateURLTestLocked(urlTest, urlTest.ID); err != nil {
		return err
	}

	m.urlTests[index] = urlTest

	return m.save()
}

// DeleteURLTest удаляет urltest с указанным ID.
func (m *Manager) DeleteURLTest(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.urlTestIndexLocked(id)
	if index < 0 {
		return ErrURLTestNotFound
	}

	m.urlTests = slices.Delete(m.urlTests, index, index+1)

	return m.save()
}

// validateURLTestLocked проверяет urltest и уникальность его тега. exceptID — ID редактируемого urltest-а
// (без блокировки).
func (m *Manager) validateURLTestLocked(urlTest URLTest, exceptID string) error {
	if err := urlTest.Validate(); err != nil {
		return err
	}

	if index := m.urlTestIndexByTagLocked(urlTest.Tag); index >= 0 && m.urlTests[index].ID != exceptID {
		return fmt.Errorf("urltest с тегом %s уже существует", urlTest.Tag)
	}

	if slices.ContainsFunc(m.items, func(item Item) bool {
		return item.Tag() == urlTest.Tag
	}) {
		return fmt.Errorf("тег %s занят outbound-ом, добавленным вручную", urlTest.Tag)
	}

	return nil
}

// urlTestIndexLocked возвращает индекс urltest-а с ID id или -1 (без блокировки).
func (m *Manager) urlTestIndexLocked(id string) int {
	return slices.IndexFunc(m.urlTests, func(urlTest URLTest) bool {
		return urlTest.ID == id
	})
}

// urlTestIndexByTagLocked возвращает индекс urltest-а с тегом tag или -1 (без блокировки).
func (m *Manager) urlTestIndexByTagLocked(tag string) int {
	return slices.IndexFunc(m.urlTests, func(urlTest URLTest) bool {
		return urlTest.Tag == tag
	})
}

// cloneURLTest копирует urltest вместе со списками.
func cloneURLTest(urlTest URLTest) URLTest {
	urlTest.Sources = slices.Clone(urlTest.Sources)
	urlTest.Tags = slices.Clone(urlTest.Tags)
	urlTest.ExcludeTags = slices.Clone(urlTest.ExcludeTags)

	return urlTest
}
