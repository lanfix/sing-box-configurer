package migrations

import (
	"fmt"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

// registry — список всех миграций. Миграции никогда не удаляются и не меняются после релиза,
// новые добавляются в конец со следующим номером версии.
//
// Миграция должна:
//   - менять только state.AppData и/или конфиг из state.SingBoxConfig();
//   - вызывать state.MarkSingBoxConfigChanged(), если изменила конфиг sing-box;
//   - возвращать ошибку, если данные нельзя привести к новому формату (обновление откатится).
var registry = []Migration{
	{
		Version: 1,
		Name:    "introduce schema_version",
		Up: func(_ *State) error {
			// Данные до появления миграций уже в актуальном формате, фиксируем только версию схемы.
			return nil
		},
	},
	{
		Version: 2,
		Name:    "add tunnel bypass rule-set",
		Up: func(state *State) error {
			config, err := state.SingBoxConfig()
			if err != nil {
				return err
			}

			if err = singboxconfig.EnsureBypass(config); err != nil {
				return fmt.Errorf("cannot add bypass to sing-box config: %w", err)
			}

			state.MarkSingBoxConfigChanged()

			return nil
		},
	},
}
