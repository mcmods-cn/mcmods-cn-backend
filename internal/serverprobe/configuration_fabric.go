package serverprobe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

const fabricDirectRegistryChannel = "fabric:registry/sync/direct"

// Fabric transmits a namespace once for a group of paths. Keep that grouping so
// retaining the decoded data costs O(wire bytes + number of identifiers).
type fabricRegistryGroup struct {
	namespace string
	paths     []string
}

func parseFabricRegistrySync(data []byte) ([]fabricRegistryGroup, []fabricRegistryGroup, error) {
	return parseFabricRegistrySyncWithBudget(data, newConfigurationParseBudget())
}

func parseFabricRegistrySyncWithBudget(data []byte, budget *configurationParseBudget) ([]fabricRegistryGroup, []fabricRegistryGroup, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty Fabric registry sync")
	}

	reader := bytes.NewReader(data)
	namespaceGroupCount, err := readBoundedConfigurationVarInt(reader, "registry namespace group", 4096)
	if err != nil {
		return nil, nil, err
	}
	if err = budget.claim("registry namespace group", namespaceGroupCount, reader.Len(), 2); err != nil {
		return nil, nil, err
	}

	var registryNames, entries []fabricRegistryGroup
	for groupIndex := int32(0); groupIndex < namespaceGroupCount; groupIndex++ {
		registryNamespace, err := readProtocolString(reader, maxConfigurationPacketBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("registry namespace group %d: %w", groupIndex, err)
		}
		registryNamespace = unoptimizeFabricNamespace(registryNamespace)

		registryCount, err := readBoundedConfigurationVarInt(reader, "registry", maximumConfigurationIdentifiers)
		if err != nil {
			return nil, nil, fmt.Errorf("registry namespace %q: %w", registryNamespace, err)
		}
		if err = budget.claim("registry", registryCount, reader.Len(), 2); err != nil {
			return nil, nil, fmt.Errorf("registry namespace %q: %w", registryNamespace, err)
		}
		registryGroup := fabricRegistryGroup{namespace: registryNamespace}
		for registryIndex := int32(0); registryIndex < registryCount; registryIndex++ {
			registryPath, err := readProtocolString(reader, maxConfigurationPacketBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("registry %s #%d path: %w", registryNamespace, registryIndex, err)
			}
			registryGroup.paths = append(registryGroup.paths, registryPath)

			entryNamespaceCount, err := readBoundedConfigurationVarInt(reader, "entry namespace", maximumConfigurationIdentifiers)
			if err != nil {
				return nil, nil, fmt.Errorf("registry %s:%s: %w", registryNamespace, registryPath, err)
			}
			if err = budget.claim("entry namespace", entryNamespaceCount, reader.Len(), 2); err != nil {
				return nil, nil, fmt.Errorf("registry %s:%s: %w", registryNamespace, registryPath, err)
			}
			for entryNamespaceIndex := int32(0); entryNamespaceIndex < entryNamespaceCount; entryNamespaceIndex++ {
				entryNamespace, err := readProtocolString(reader, maxConfigurationPacketBytes)
				if err != nil {
					return nil, nil, fmt.Errorf(
						"registry %s:%s entry namespace %d: %w",
						registryNamespace,
						registryPath,
						entryNamespaceIndex,
						err,
					)
				}
				entryNamespace = unoptimizeFabricNamespace(entryNamespace)

				bulkCount, err := readBoundedConfigurationVarInt(reader, "raw id bulk", maximumConfigurationIdentifiers)
				if err != nil {
					return nil, nil, fmt.Errorf(
						"registry %s:%s namespace %s: %w",
						registryNamespace,
						registryPath,
						entryNamespace,
						err,
					)
				}
				if err = budget.claim("raw id bulk", bulkCount, reader.Len(), 2); err != nil {
					return nil, nil, fmt.Errorf("registry %s:%s namespace %s: %w",
						registryNamespace, registryPath, entryNamespace, err)
				}
				entryGroup := fabricRegistryGroup{namespace: entryNamespace}
				for bulkIndex := int32(0); bulkIndex < bulkCount; bulkIndex++ {
					if _, err = readVarInt(reader); err != nil {
						return nil, nil, fmt.Errorf(
							"registry %s:%s namespace %s bulk %d raw id delta: %w",
							registryNamespace,
							registryPath,
							entryNamespace,
							bulkIndex,
							err,
						)
					}
					bulkSize, err := readBoundedConfigurationVarInt(reader, "raw id bulk entry", maximumConfigurationIdentifiers)
					if err != nil {
						return nil, nil, fmt.Errorf(
							"registry %s:%s namespace %s bulk %d: %w",
							registryNamespace,
							registryPath,
							entryNamespace,
							bulkIndex,
							err,
						)
					}
					if err = budget.claim("raw id bulk entry", bulkSize, reader.Len(), 1); err != nil {
						return nil, nil, err
					}
					for entryIndex := int32(0); entryIndex < bulkSize; entryIndex++ {
						entryPath, err := readProtocolString(reader, maxConfigurationPacketBytes)
						if err != nil {
							return nil, nil, fmt.Errorf(
								"registry %s:%s namespace %s bulk %d entry %d: %w",
								registryNamespace,
								registryPath,
								entryNamespace,
								bulkIndex,
								entryIndex,
								err,
							)
						}
						entryGroup.paths = append(entryGroup.paths, entryPath)
					}
				}
				if len(entryGroup.paths) > 0 {
					entries = append(entries, entryGroup)
				}
			}
		}
		if len(registryGroup.paths) > 0 {
			registryNames = append(registryNames, registryGroup)
		}
	}
	if reader.Len() != 0 {
		padding := make([]byte, reader.Len())
		_, _ = io.ReadFull(reader, padding)
		for _, value := range padding {
			if value != 0 {
				return nil, nil, fmt.Errorf("%d non-padding trailing bytes in Fabric registry sync", len(padding))
			}
		}
	}
	return registryNames, entries, nil
}

func recordFabricRegistryNamespaces(target map[string]int, groups []fabricRegistryGroup) error {
	for _, group := range groups {
		namespace, _, _ := strings.Cut(group.namespace, ":")
		if err := recordConfigurationNamespace(target, strings.ToLower(namespace), len(group.paths)); err != nil {
			return err
		}
	}
	return nil
}

func readBoundedConfigurationVarInt(reader io.ByteReader, label string, maximum int32) (int32, error) {
	value, err := readVarInt(reader)
	if err != nil {
		return 0, err
	}
	if value < 0 || value > maximum {
		return 0, fmt.Errorf("invalid %s count %d", label, value)
	}
	return value, nil
}

func unoptimizeFabricNamespace(namespace string) string {
	if namespace == "" {
		return "minecraft"
	}
	return namespace
}
