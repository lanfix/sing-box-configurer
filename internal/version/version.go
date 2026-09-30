// Package version содержит версию приложения, которая задается при сборке образа.
package version

// Version задается при сборке через -ldflags "-X .../internal/version.Version=v1.2.3".
// Версия приложения совпадает с тегом релиза и docker-образа.
var Version = "dev"
