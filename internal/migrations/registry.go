package migrations

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
}
