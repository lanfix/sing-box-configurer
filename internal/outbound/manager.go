package outbound

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

// Manager управляет VPN outbounds в sing-box конфиге.
type Manager struct {
	configManager *singboxconfig.Provider
	mu            sync.RWMutex
}

// NewManager создает новый менеджер outbounds.
func NewManager(configManager *singboxconfig.Provider) *Manager {
	return &Manager{
		configManager: configManager,
	}
}

// GetOutbounds возвращает список всех VPN outbounds из конфига (включая endpoints).
func (m *Manager) GetOutbounds() ([]Outbound, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configData, err := m.configManager.GetActualConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get config: %w", err)
	}

	if m.configManager.HasPending() {
		tempConfigData, err := m.configManager.GetTempConfig()
		if err != nil {
			return nil, fmt.Errorf("cannot get temp config: %w", err)
		}

		configData = tempConfigData
	}

	var config map[string]interface{}

	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("cannot parse config json: %w", err)
	}

	var result []Outbound

	outboundsRaw, ok := config["outbounds"]
	if ok {
		outboundsList, ok := outboundsRaw.([]interface{})
		if ok {
			for _, item := range outboundsList {
				outboundMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				outboundType, _ := outboundMap["type"].(string)
				tag, _ := outboundMap["tag"].(string)

				if outboundType == "selector" || outboundType == "direct" || outboundType == "block" {
					continue
				}

				server, _ := outboundMap["server"].(string)
				port := 0

				if serverPort, ok := outboundMap["server_port"].(float64); ok {
					port = int(serverPort)
				}

				result = append(result, Outbound{
					Tag:        tag,
					Type:       outboundType,
					Server:     server,
					Port:       port,
					Config:     outboundMap,
					IsEndpoint: false,
				})
			}
		}
	}

	endpointsRaw, ok := config["endpoints"]
	if ok {
		endpointsList, ok := endpointsRaw.([]interface{})
		if ok {
			for _, item := range endpointsList {
				endpointMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				endpointType, _ := endpointMap["type"].(string)
				tag, _ := endpointMap["tag"].(string)

				server := ""
				port := 0

				if endpointType == "wireguard" {
					peersRaw, ok := endpointMap["peers"].([]interface{})
					if ok && len(peersRaw) > 0 {
						if peer, ok := peersRaw[0].(map[string]interface{}); ok {
							server, _ = peer["address"].(string)

							if peerPort, ok := peer["port"].(float64); ok {
								port = int(peerPort)
							}
						}
					}
				}

				result = append(result, Outbound{
					Tag:        tag,
					Type:       endpointType,
					Server:     server,
					Port:       port,
					Config:     endpointMap,
					IsEndpoint: true,
				})
			}
		}
	}

	return result, nil
}

// AddOutboundFromShare добавляет новый outbound в конфиг из share-ссылки.
func (m *Manager) AddOutboundFromShare(shareUrl string) (*Outbound, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	share, err := ParseShareUrl(shareUrl)
	if err != nil {
		return nil, fmt.Errorf("cannot parse share url: %w", err)
	}

	configStr, err := m.configManager.GetActualConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	var config map[string]interface{}

	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, fmt.Errorf("cannot parse config json: %w", err)
	}

	outbound := share.GetOutbound()

	if outbound.IsEndpoint {
		return m.addEndpoint(config, outbound)
	}

	return m.addOutboundToConfig(config, outbound)
}

// addEndpoint добавляет endpoint (например, WireGuard) в секцию endpoints.
func (m *Manager) addEndpoint(config map[string]interface{}, outbound *Outbound) (*Outbound, error) {
	endpointsRaw, ok := config["endpoints"]
	if !ok {
		config["endpoints"] = []interface{}{}
		endpointsRaw = config["endpoints"]
	}

	endpointsList, ok := endpointsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid endpoints format")
	}

	for _, item := range endpointsList {
		endpointMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		existingTag, _ := endpointMap["tag"].(string)
		if existingTag == outbound.Tag {
			outbound.Tag = fmt.Sprintf("%s-%s", outbound.Tag, uuid.New().String()[:8])
			outbound.Config["tag"] = outbound.Tag

			break
		}
	}

	endpointsList = append(endpointsList, outbound.Config)
	config["endpoints"] = endpointsList

	selectorIndex := -1
	outboundsRaw, ok := config["outbounds"]
	if ok {
		outboundsList, ok := outboundsRaw.([]interface{})
		if ok {
			for i, item := range outboundsList {
				outboundMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				outboundType, _ := outboundMap["type"].(string)
				tag, _ := outboundMap["tag"].(string)

				if outboundType == "selector" && (tag == "select" || tag == "select-default") {
					if selectorIndex == -1 {
						selectorIndex = i
					}

					selectorsOutbounds, ok := outboundMap["outbounds"].([]interface{})
					if ok {
						found := false

						for _, selOutbound := range selectorsOutbounds {
							if selOutbound == outbound.Tag {
								found = true

								break
							}
						}

						if !found {
							selectorsOutbounds = append(selectorsOutbounds, outbound.Tag)
							outboundMap["outbounds"] = selectorsOutbounds
						}
					}
				}
			}

			config["outbounds"] = outboundsList
		}
	}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := m.configManager.SaveTempConfig(newData); err != nil {
		return nil, fmt.Errorf("cannot save temp config: %w", err)
	}

	return outbound, nil
}

// addOutboundToConfig добавляет обычный outbound в секцию outbounds.
func (m *Manager) addOutboundToConfig(config map[string]interface{}, outbound *Outbound) (*Outbound, error) {
	outboundsRaw, ok := config["outbounds"]
	if !ok {
		config["outbounds"] = []interface{}{}
		outboundsRaw = config["outbounds"]
	}

	outboundsList, ok := outboundsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid outbounds format")
	}

	for _, item := range outboundsList {
		outboundMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		existingTag, _ := outboundMap["tag"].(string)
		if existingTag == outbound.Tag {
			outbound.Tag = fmt.Sprintf("%s-%s", outbound.Tag, uuid.New().String()[:8])
			outbound.Config["tag"] = outbound.Tag

			break
		}
	}

	selectorIndex := -1
	insertPosition := len(outboundsList)

	for i, item := range outboundsList {
		outboundMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		outboundType, _ := outboundMap["type"].(string)
		tag, _ := outboundMap["tag"].(string)

		if outboundType == "selector" && (tag == "select" || tag == "select-default") {
			if selectorIndex == -1 {
				selectorIndex = i
			}

			selectorsOutbounds, ok := outboundMap["outbounds"].([]interface{})
			if ok {
				found := false

				for _, selOutbound := range selectorsOutbounds {
					if selOutbound == outbound.Tag {
						found = true

						break
					}
				}

				if !found {
					selectorsOutbounds = append(selectorsOutbounds, outbound.Tag)
					outboundMap["outbounds"] = selectorsOutbounds
				}
			}
		}

		if outboundType == "direct" || outboundType == "block" {
			if insertPosition == len(outboundsList) {
				insertPosition = i
			}
		}
	}

	newOutboundsList := make([]interface{}, 0, len(outboundsList)+1)
	newOutboundsList = append(newOutboundsList, outboundsList[:insertPosition]...)
	newOutboundsList = append(newOutboundsList, outbound.Config)
	newOutboundsList = append(newOutboundsList, outboundsList[insertPosition:]...)

	config["outbounds"] = newOutboundsList

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := m.configManager.SaveTempConfig(newData); err != nil {
		return nil, fmt.Errorf("cannot save temp config: %w", err)
	}

	return outbound, nil
}

// DeleteOutbound удаляет outbound из конфига по тегу (из outbounds или endpoints).
func (m *Manager) DeleteOutbound(tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}

	configStr, err := m.configManager.GetActualConfig()
	if err != nil {
		return fmt.Errorf("cannot read config: %w", err)
	}

	var config map[string]interface{}

	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return fmt.Errorf("cannot parse config json: %w", err)
	}

	found := false

	outboundsRaw, ok := config["outbounds"]
	if ok {
		outboundsList, ok := outboundsRaw.([]interface{})
		if ok {
			newOutboundsList := make([]interface{}, 0, len(outboundsList))

			for _, item := range outboundsList {
				outboundMap, ok := item.(map[string]interface{})
				if !ok {
					newOutboundsList = append(newOutboundsList, item)

					continue
				}

				outboundTag, _ := outboundMap["tag"].(string)
				outboundType, _ := outboundMap["type"].(string)

				if outboundTag == tag {
					found = true

					continue
				}

				if outboundType == "selector" {
					selectorsOutbounds, ok := outboundMap["outbounds"].([]interface{})
					if ok {
						newSelectorsOutbounds := make([]interface{}, 0, len(selectorsOutbounds))

						for _, selOutbound := range selectorsOutbounds {
							if selOutbound != tag {
								newSelectorsOutbounds = append(newSelectorsOutbounds, selOutbound)
							}
						}

						outboundMap["outbounds"] = newSelectorsOutbounds
					}
				}

				newOutboundsList = append(newOutboundsList, outboundMap)
			}

			config["outbounds"] = newOutboundsList
		}
	}

	endpointsRaw, ok := config["endpoints"]
	if ok {
		endpointsList, ok := endpointsRaw.([]interface{})
		if ok {
			newEndpointsList := make([]interface{}, 0, len(endpointsList))

			for _, item := range endpointsList {
				endpointMap, ok := item.(map[string]interface{})
				if !ok {
					newEndpointsList = append(newEndpointsList, item)

					continue
				}

				endpointTag, _ := endpointMap["tag"].(string)

				if endpointTag == tag {
					found = true

					outboundsRaw2, ok2 := config["outbounds"]
					if ok2 {
						outboundsList2, ok3 := outboundsRaw2.([]interface{})
						if ok3 {
							for _, item2 := range outboundsList2 {
								outboundMap2, ok4 := item2.(map[string]interface{})
								if !ok4 {
									continue
								}

								outboundType2, _ := outboundMap2["type"].(string)

								if outboundType2 == "selector" {
									selectorsOutbounds2, ok5 := outboundMap2["outbounds"].([]interface{})
									if ok5 {
										newSelectorsOutbounds2 := make([]interface{}, 0, len(selectorsOutbounds2))

										for _, selOutbound2 := range selectorsOutbounds2 {
											if selOutbound2 != tag {
												newSelectorsOutbounds2 = append(newSelectorsOutbounds2, selOutbound2)
											}
										}

										outboundMap2["outbounds"] = newSelectorsOutbounds2
									}
								}
							}
						}
					}

					continue
				}

				newEndpointsList = append(newEndpointsList, endpointMap)
			}

			config["endpoints"] = newEndpointsList
		}
	}

	if !found {
		return fmt.Errorf("outbound not found")
	}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := m.configManager.SaveTempConfig(newData); err != nil {
		return fmt.Errorf("cannot save temp config: %w", err)
	}

	return nil
}
