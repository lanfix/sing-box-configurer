package outbound

import (
	"fmt"
	"net/url"
	"strings"
)

// shareTransport возвращает транспорт sing-box по параметрам share-ссылки (type, path, host, serviceName).
// Поля транспорта в sing-box лежат на одном уровне с type. Для TCP транспорт не нужен и возвращается nil.
func shareTransport(params url.Values) (map[string]any, error) {
	path := params.Get("path")
	host := params.Get("host")

	switch transportType := coalesce(params.Get("type"), "tcp"); transportType {
	case "tcp", "raw":
		return nil, nil

	case "ws":
		transport := map[string]any{
			"type": "ws",
		}

		if path != "" {
			transport["path"] = path
		}

		if host != "" {
			transport["headers"] = map[string]any{
				"Host": host,
			}
		}

		return transport, nil

	case "httpupgrade":
		transport := map[string]any{
			"type": "httpupgrade",
		}

		if path != "" {
			transport["path"] = path
		}

		if host != "" {
			transport["host"] = host
		}

		return transport, nil

	case "http", "h2":
		transport := map[string]any{
			"type": "http",
		}

		if path != "" {
			transport["path"] = path
		}

		if host != "" {
			hosts := make([]any, 0)

			for _, item := range strings.Split(host, ",") {
				if item = strings.TrimSpace(item); item != "" {
					hosts = append(hosts, item)
				}
			}

			transport["host"] = hosts
		}

		return transport, nil

	case "grpc":
		transport := map[string]any{
			"type": "grpc",
		}

		if serviceName := coalesce(params.Get("serviceName"), path); serviceName != "" {
			transport["service_name"] = serviceName
		}

		return transport, nil

	case "xhttp", "splithttp":
		return nil, fmt.Errorf("транспорт %s не поддерживается sing-box", transportType)

	default:
		return nil, fmt.Errorf("неизвестный транспорт %q", transportType)
	}
}
